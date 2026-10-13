package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	readability "github.com/go-shiori/go-readability"
)

// articleClient talks HTTP/1.1 with a browser User-Agent. Go's default client
// (HTTP/2, "Go-http-client") is rejected by many publishers' bot filters, which
// show up as "stream error ... INTERNAL_ERROR" or 403s.
var articleClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		Proxy:             http.ProxyFromEnvironment,
		ForceAttemptHTTP2: false,
		TLSNextProto:      map[string]func(string, *tls.Conn) http.RoundTripper{},
	},
}

// fetchedArticle is the readable text of a page plus the links inside the article body.
type fetchedArticle struct {
	Text  string
	Links []string
}

// fetchArticleText downloads pageURL and returns the readable article text.
func fetchArticleText(pageURL string) (string, error) {
	a, err := fetchArticle(pageURL)
	return a.Text, err
}

// fetchArticle downloads pageURL and returns the readable article text and the absolute
// http(s) links in the article body (the plain text drops link targets).
func fetchArticle(pageURL string) (fetchedArticle, error) {
	req, err := http.NewRequest(http.MethodGet, pageURL, nil)
	if err != nil {
		return fetchedArticle{}, err
	}
	req.Header.Set("User-Agent", googleNewsUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := articleClient.Do(req)
	if err != nil {
		return fetchedArticle{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fetchedArticle{}, fmt.Errorf("status %d", resp.StatusCode)
	}
	article, err := readability.FromReader(io.LimitReader(resp.Body, 5<<20), resp.Request.URL)
	if err != nil {
		return fetchedArticle{}, err
	}
	return fetchedArticle{Text: article.TextContent, Links: hrefsIn(article.Content)}, nil
}

// hrefsIn returns the absolute http(s) link targets in an HTML fragment.
func hrefsIn(htmlFragment string) []string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlFragment))
	if err != nil {
		return nil
	}
	var links []string
	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		if href := strings.TrimSpace(s.AttrOr("href", "")); strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
			links = append(links, href)
		}
	})
	return links
}
