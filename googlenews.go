package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Overridable in tests.
var (
	googleNewsBase  = "https://news.google.com"
	googleNewsDelay = 700 * time.Millisecond // be gentle: one decode per story
)

const googleNewsUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"

var (
	gnSigRe = regexp.MustCompile(`data-n-a-sg="([^"]+)"`)
	gnTsRe  = regexp.MustCompile(`data-n-a-ts="(\d+)"`)
)

func isGoogleNewsLink(link string) bool {
	return strings.Contains(link, "news.google.com/")
}

// googleNewsID returns the article ID from news.google.com/(rss/)articles/<id> or /read/<id> links.
func googleNewsID(link string) (string, bool) {
	u, err := url.Parse(link)
	if err != nil || u.Host != "news.google.com" {
		return "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return "", false
	}
	if kind := parts[len(parts)-2]; kind != "articles" && kind != "read" {
		return "", false
	}
	return parts[len(parts)-1], true
}

// decodeGoogleNewsURL resolves a Google News article link to the publisher URL.
// Current Google News IDs are encrypted tokens, so this asks Google's own web
// endpoint (the one the news.google.com page calls) to decode it. That endpoint is
// undocumented and can change or rate-limit without notice; errors are returned
// so the caller can fall back to the headline.
func decodeGoogleNewsURL(link string) (string, error) {
	id, ok := googleNewsID(link)
	if !ok {
		return "", fmt.Errorf("not a google news article link")
	}
	client := &http.Client{Timeout: 30 * time.Second}
	time.Sleep(googleNewsDelay)

	// 1. The article page carries a per-article signature and timestamp.
	req, _ := http.NewRequest(http.MethodGet, googleNewsBase+"/rss/articles/"+id+"?hl=en-US&gl=US&ceid=US:en", nil)
	req.Header.Set("User-Agent", googleNewsUA)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	page, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	resp.Body.Close()
	if err != nil {
		return "", err
	}
	sig, ts := gnSigRe.FindSubmatch(page), gnTsRe.FindSubmatch(page)
	if sig == nil || ts == nil {
		return "", fmt.Errorf("no decoding parameters on article page (status %d; consent or changed page?)", resp.StatusCode)
	}

	// 2. Ask the batchexecute endpoint for the target URL.
	idJSON, _ := json.Marshal(id)
	sigJSON, _ := json.Marshal(string(sig[1]))
	args := fmt.Sprintf(`["garturlreq",[["X","X",["X","X"],null,null,1,1,"US:en",null,1,null,null,null,null,null,0,1],"X","X",1,[1,1,1],1,1,null,0,0,null,0],%s,%s,%s]`, idJSON, ts[1], sigJSON)
	freq, _ := json.Marshal([][]interface{}{{[]interface{}{"Fbv4je", args, nil, "generic"}}})

	req, _ = http.NewRequest(http.MethodPost, googleNewsBase+"/_/DotsSplashUi/data/batchexecute",
		strings.NewReader("f.req="+url.QueryEscape(string(freq))))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=UTF-8")
	req.Header.Set("User-Agent", googleNewsUA)
	resp, err = client.Do(req)
	if err != nil {
		return "", err
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	resp.Body.Close()
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("batchexecute status %d", resp.StatusCode)
	}
	return parseBatchExecuteURL(string(body))
}

// parseBatchExecuteURL extracts the URL from a batchexecute response:
// )]}'  then [["wrb.fr","Fbv4je","[\"garturlres\",\"<url>\",1]",...],...]
func parseBatchExecuteURL(body string) (string, error) {
	i := strings.Index(body, "[[")
	if i < 0 {
		return "", fmt.Errorf("unexpected batchexecute response")
	}
	var outer [][]interface{}
	if err := json.NewDecoder(strings.NewReader(body[i:])).Decode(&outer); err != nil {
		return "", fmt.Errorf("batchexecute json: %w", err)
	}
	for _, row := range outer {
		if len(row) < 3 || row[0] != "wrb.fr" {
			continue
		}
		payload, _ := row[2].(string)
		var inner []interface{}
		if json.Unmarshal([]byte(payload), &inner) != nil || len(inner) < 2 {
			continue
		}
		if target, _ := inner[1].(string); strings.HasPrefix(target, "http") {
			return target, nil
		}
	}
	return "", fmt.Errorf("no url in batchexecute response")
}

// googleNewsArticleText decodes a Google News link and returns the publisher article's text.
func googleNewsArticleText(link string) (string, error) {
	target, err := decodeGoogleNewsURL(link)
	if err != nil {
		return "", fmt.Errorf("decode google news link: %w", err)
	}
	text, err := fetchArticleText(target)
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", target, err)
	}
	return text, nil
}

// unwrapGoogleRedirect returns the publisher URL for Google Alerts links of the form
// https://www.google.com/url?...&url=<publisher>&..., which carry the real target in
// a query parameter (no scraping needed). Other links are returned unchanged.
func unwrapGoogleRedirect(link string) string {
	u, err := url.Parse(link)
	if err != nil || !strings.HasSuffix(u.Host, "google.com") || u.Path != "/url" {
		return link
	}
	q := u.Query()
	for _, key := range []string{"url", "q"} {
		if target := q.Get(key); strings.HasPrefix(target, "http") {
			return target
		}
	}
	return link
}
