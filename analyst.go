package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/mmcdole/gofeed"
)

const (
	analystTag   = "#analyst"
	defaultLimit = 20
)

func RunAnalyst(cfg *Config, store *Storage, llm *LLMClient, writer Writer, fp *gofeed.Parser) {
	if len(cfg.AnalystFeeds) == 0 || cfg.AnalystPrompt == "" {
		return
	}
	if llm == nil {
		return
	}

	items := collectArticlesForAnalysis(cfg, store, fp)
	if len(items) == 0 {
		return
	}

	deepDive := cfg.AnalystReadPapers && cfg.AnalystMaxPapers > 0
	articles := make([]string, len(items))
	for i, it := range items {
		if deepDive {
			articles[i] = fmt.Sprintf("[%d] %s: %s", i+1, it.Title, it.Description)
		} else {
			articles[i] = it.Title + ": " + it.Description
		}
	}

	suffix := ""
	if deepDive {
		suffix = fmt.Sprintf("\n\nEach article below is numbered. After your analysis, add a final line of the form `SELECTED: 2, 5` listing the numbers of at most %d articles worth reading in full, or `SELECTED: none`.", cfg.AnalystMaxPapers)
	}

	analysis := llm.Analyze(articles, suffix)

	var selected []analystItem
	if deepDive {
		analysis, selected = splitSelection(analysis, items, cfg.AnalystMaxPapers)
	}

	if analysis != "" {
		writer.Write("\n## Daily Analysis:\n")
		writer.Write(analysis + "\n")
		writeSelectedPapers(cfg, store, llm, writer, selected)

		if cfg.NotificationTrigger != "" &&
			cfg.NotificationWebhookURL != "" &&
			strings.TrimSpace(analysis) == strings.TrimSpace(cfg.NotificationTrigger) &&
			!store.WasFeedNotifiedToday(cfg.AnalystFeeds[0]) {

			if err := sendNotification(cfg.NotificationWebhookURL, analysis); err == nil {
				_ = store.MarkFeedNotified(cfg.AnalystFeeds[0])
			}
		}
	}
}

// analystItem is an article handed to the Analyst, kept so its selections can be resolved back to links.
type analystItem struct {
	Title       string
	Link        string
	Description string
	FeedTitle   string
	FeedURL     string
}

var selectedLineRe = regexp.MustCompile(`(?im)^\W*SELECTED:\s*(.*)$`)

// splitSelection removes the trailing "SELECTED: n, m" line from the analysis and
// returns the chosen items. Numbers are 1-based indexes into items; anything
// out of range or repeated is ignored, so a hallucinated number can't pick a paper.
func splitSelection(analysis string, items []analystItem, max int) (string, []analystItem) {
	locs := selectedLineRe.FindAllStringSubmatchIndex(analysis, -1)
	if len(locs) == 0 {
		return analysis, nil
	}
	last := locs[len(locs)-1]
	list := analysis[last[2]:last[3]]
	cleaned := strings.TrimSpace(analysis[:last[0]] + analysis[last[1]:])

	var picked []analystItem
	seen := map[int]bool{}
	for _, tok := range regexp.MustCompile(`\d+`).FindAllString(list, -1) {
		n, err := strconv.Atoi(tok)
		if err != nil || n < 1 || n > len(items) || seen[n] {
			continue
		}
		seen[n] = true
		picked = append(picked, items[n-1])
		if len(picked) == max {
			break
		}
	}
	return cleaned, picked
}

// writeSelectedPapers summarizes the Analyst's picks from full text (MinerU) and
// writes them under the analysis. Summaries are cached under the plain link.
func writeSelectedPapers(cfg *Config, store *Storage, llm *LLMClient, w Writer, picks []analystItem) {
	var out string
	for _, it := range picks {
		if _, ok := arxivPDFURL(it.Link); !ok {
			continue
		}
		seen, _, summary := store.IsSeen(it.Link)
		if summary == "" {
			text := getPaperMarkdown(it.Link, cfg)
			if text == "" {
				text = it.Description // MinerU unavailable: fall back to the abstract
			}
			summary = llm.SummarizePaper(text)
			if summary != "" && !seen {
				_ = store.MarkAsSeen(it.Link, summary, it.Title, it.FeedTitle, it.FeedURL)
			}
		}
		out += w.WriteLink(it.Title, it.Link, true, "") + w.WriteSummary(summary, true)
	}
	if out != "" {
		w.Write("\n### Papers worth reading in full:\n" + out)
	}
}

func collectArticlesForAnalysis(cfg *Config, store *Storage, fp *gofeed.Parser) []analystItem {
	var articles []analystItem

	for _, feedSpec := range cfg.AnalystFeeds {
		// Accept "URL N" like the other feed lists
		feedURL, limit := getFeedAndLimit(feedSpec)
		feed, err := fp.ParseURL(feedURL)
		if err != nil {
			continue
		}

		// Limit items
		if len(feed.Items) > limit {
			feed.Items = feed.Items[:limit]
		}

		for _, item := range feed.Items {
			articleLink := item.Link + analystTag
			seen, seenToday, summary := store.IsSeen(articleLink)

			if seen {
				continue
			}

			articles = append(articles, analystItem{
				Title: item.Title, Link: item.Link, Description: item.Description,
				FeedTitle: feed.Title, FeedURL: feed.Link,
			})

			if !seenToday {
				if err := store.MarkAsSeen(articleLink, summary, item.Title, feed.Title, feed.Link); err != nil {
					continue
				}
			}
		}
	}

	return articles
}

func sendNotification(url, message string) error {
	// Detect Slack webhook
	if strings.Contains(url, "hooks.slack.com") {
		// Slack requires JSON payload
		payload := map[string]string{"text": message}
		body, _ := json.Marshal(payload)

		resp, err := http.Post(url, "application/json", bytes.NewReader(body))
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		io.Copy(io.Discard, resp.Body)
		return nil
	}

	// Default: ntfy.sh or generic text webhook
	resp, err := http.Post(url, "text/plain", strings.NewReader(message))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return nil
}
