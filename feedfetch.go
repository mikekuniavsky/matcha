package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
)

// Feeds are fetched here, not with gofeed's ParseURL, so Matcha can identify itself, space
// out requests to hosts with rate limits (arXiv's API asks for one request every 3 seconds),
// and retry politely when a host answers 429 Too Many Requests or 503.

const feedUA = "Matcha/1.0 (+https://github.com/mikekuniavsky/matcha; personal RSS digest)"

var (
	feedClient = &http.Client{Timeout: 60 * time.Second}

	// Minimum gap between requests to the same host.
	minHostInterval = map[string]time.Duration{
		"export.arxiv.org": 3 * time.Second,
		"rss.arxiv.org":    3 * time.Second,
		"arxiv.org":        3 * time.Second,
	}
	hostLastRequest = map[string]time.Time{}

	// Waits between attempts after a 429/503 when the server gives no Retry-After.
	retryDelays   = []time.Duration{5 * time.Second, 20 * time.Second, 60 * time.Second}
	maxRetryAfter = 2 * time.Minute

	sleepFor = time.Sleep
)

// feedResult describes how a fetch went, for the log and for -check-feeds.
type feedResult struct {
	Status   int // HTTP status of the last response (0 if none)
	Attempts int
}

// politeWait blocks until at least minHostInterval has passed since the last request to host.
func politeWait(host string) {
	if gap := minHostInterval[host]; gap > 0 {
		if wait := gap - time.Since(hostLastRequest[host]); wait > 0 {
			sleepFor(wait)
		}
	}
	hostLastRequest[host] = time.Now()
}

// retryAfter parses a Retry-After header (seconds or an HTTP date), capped at maxRetryAfter.
func retryAfter(h string) (time.Duration, bool) {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0, false
	}
	var d time.Duration
	if secs, err := strconv.Atoi(h); err == nil {
		d = time.Duration(secs) * time.Second
	} else if t, err := http.ParseTime(h); err == nil {
		d = time.Until(t)
	} else {
		return 0, false
	}
	if d < 0 {
		d = 0
	}
	if d > maxRetryAfter {
		d = maxRetryAfter
	}
	return d, true
}

// fetchFeed downloads and parses a feed, retrying on 429 and 503.
func fetchFeed(fp *gofeed.Parser, feedURL string) (*gofeed.Feed, feedResult, error) {
	var res feedResult
	u, err := url.Parse(feedURL)
	if err != nil {
		return nil, res, err
	}
	for attempt := 0; ; attempt++ {
		politeWait(u.Hostname())
		req, err := http.NewRequest(http.MethodGet, feedURL, nil)
		if err != nil {
			return nil, res, err
		}
		req.Header.Set("User-Agent", feedUA)
		req.Header.Set("Accept", "application/atom+xml, application/rss+xml, application/xml;q=0.9, */*;q=0.8")

		resp, err := feedClient.Do(req)
		res.Attempts++
		if err != nil {
			return nil, res, err
		}
		res.Status = resp.StatusCode
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		resp.Body.Close()

		if (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable) && attempt < len(retryDelays) {
			wait, ok := retryAfter(resp.Header.Get("Retry-After"))
			if !ok {
				wait = retryDelays[attempt]
			}
			fmt.Printf("  %s answered %s; waiting %s before retry %d of %d\n", u.Hostname(), resp.Status, wait.Round(time.Second), attempt+1, len(retryDelays))
			sleepFor(wait)
			continue
		}
		if resp.StatusCode >= 400 {
			return nil, res, fmt.Errorf("http error: %s", resp.Status)
		}
		if readErr != nil {
			return nil, res, readErr
		}
		feed, err := fp.ParseString(string(body))
		return feed, res, err
	}
}

// runFeedCheck fetches every configured feed once (same pacing and retries as a normal run)
// and prints what came back. It writes no digest, calls no LLM and does not touch the database.
func runFeedCheck(cfg *Config) {
	fp := gofeed.NewParser()
	check := func(label, feedURL string) {
		start := time.Now()
		feed, res, err := fetchFeed(fp, feedURL)
		took := time.Since(start).Round(100 * time.Millisecond)
		switch {
		case err != nil:
			fmt.Printf("FAIL  %-4d %d attempt(s)  %6s  %s  (%v)\n", res.Status, res.Attempts, took, label, err)
		default:
			fmt.Printf("ok    %-4d %d attempt(s)  %6s  %s  (%d items)\n", res.Status, res.Attempts, took, label, len(feed.Items))
		}
	}
	for _, f := range cfg.Feeds {
		label := f.name
		if label == "" {
			label = shorten(f.url, 90)
		}
		check(label, f.url)
	}
	for _, spec := range cfg.AnalystFeeds {
		feedURL, _ := getFeedAndLimit(spec)
		check("analyst: "+shorten(feedURL, 80), feedURL)
	}
}
