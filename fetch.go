package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"time"

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

// fetchArticleText downloads pageURL and returns the readable article text.
func fetchArticleText(pageURL string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, pageURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", googleNewsUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := articleClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	article, err := readability.FromReader(io.LimitReader(resp.Body, 5<<20), resp.Request.URL)
	if err != nil {
		return "", err
	}
	return article.TextContent, nil
}
