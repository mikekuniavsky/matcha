package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	readability "github.com/go-shiori/go-readability"
)

func isGoogleNewsLink(link string) bool {
	return strings.Contains(link, "news.google.com/")
}

// googleNewsArticleText fetches the publisher article behind a Google News
// redirect link and returns its text. Google sometimes serves a JavaScript or
// consent page instead of redirecting; in that case it returns an error so the
// caller can skip the summary rather than summarize Google's boilerplate.
func googleNewsArticleText(link string) (string, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest(http.MethodGet, link, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Matcha)")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	final := resp.Request.URL
	if isGoogleNewsLink(final.String()) || strings.HasSuffix(final.Host, "google.com") {
		return "", fmt.Errorf("google news did not redirect to the publisher (%s)", final.Host)
	}
	article, err := readability.FromReader(resp.Body, final)
	if err != nil {
		return "", err
	}
	return article.TextContent, nil
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
