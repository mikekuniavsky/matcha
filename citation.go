package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/spf13/viper"
)

// CitationWatch is one entry of `citation_watch` in config.yaml: papers whose new citations you
// want to follow. Matcha asks OpenAlex for works that cite any of `papers` and treats the result
// like an arXiv search: listed (use: feed or summary) or screened by an analyst (use: analyst).
type CitationWatch struct {
	Name    string   `mapstructure:"name"`
	Use     string   `mapstructure:"use"`     // analyst (default) | summary | feed
	Days    int      `mapstructure:"days"`    // look back this many days by publication date (default 30)
	Limit   int      `mapstructure:"limit"`   // newest N citing papers kept
	Papers  []string `mapstructure:"papers"`  // arXiv IDs ("arXiv:2509.15246"), DOIs or OpenAlex IDs ("W123")
	Analyst string   `mapstructure:"analyst"` // use: analyst only: which analyst reads it (default the first)
}

// loadCitationWatch expands `citation_watch` into feeds and analyst feeds, like loadArxivSearches.
func loadCitationWatch(cfg *Config) (feeds []RSS, analyst []arxivAnalystFeed) {
	var watches []CitationWatch
	if err := viper.UnmarshalKey("citation_watch", &watches); err != nil {
		log.Printf("citation_watch: %v", err)
		return nil, nil
	}
	cfg.CitationWatch = watches
	for i, w := range watches {
		name := strings.TrimSpace(w.Name)
		if name == "" {
			name = fmt.Sprintf("#%d", i+1)
		}
		var papers []string
		for _, p := range w.Papers {
			if p = strings.TrimSpace(p); p == "" {
				continue
			}
			if _, err := refFilter(p); err != nil {
				log.Printf("citation_watch %s: %v (skipped)", name, err)
				continue
			}
			papers = append(papers, p)
		}
		if len(papers) == 0 {
			log.Printf("citation_watch %s: no usable papers (skipped)", name)
			continue
		}
		use := strings.ToLower(strings.TrimSpace(w.Use))
		if use == "" {
			use = "analyst"
		}
		if w.Limit <= 0 {
			w.Limit = map[string]int{"summary": 5, "analyst": 50, "feed": 30}[use]
		}
		if w.Days <= 0 {
			w.Days = 30
		}
		feedURL := citationFeedURL(name, papers, w.Days)
		switch use {
		case "summary":
			feeds = append(feeds, RSS{url: feedURL, limit: w.Limit, summarize: true, name: "Cites: " + name})
		case "feed":
			feeds = append(feeds, RSS{url: feedURL, limit: w.Limit, name: "Cites: " + name})
		case "analyst":
			analyst = append(analyst, arxivAnalystFeed{kind: "citation_watch", name: name, analyst: w.Analyst, spec: fmt.Sprintf("%s %d", feedURL, w.Limit)})
		default:
			log.Printf("citation_watch %s: use must be summary, analyst or feed (got %q) (skipped)", name, w.Use)
		}
	}
	return feeds, analyst
}
