package main

import (
	"fmt"
	"log"
	"net/url"
	"regexp"
	"strings"

	"github.com/spf13/viper"
)

// ArxivSearch is one entry of `arxiv_searches` in config.yaml. Matcha turns it into an
// arXiv query-API feed, so people edit plain terms instead of a URL-encoded search_query.
type ArxivSearch struct {
	Name       string   `mapstructure:"name"`
	Use        string   `mapstructure:"use"`        // summary | analyst | feed
	Limit      int      `mapstructure:"limit"`      // newest N results kept
	Field      string   `mapstructure:"field"`      // all (default) | ti | abs
	Terms      []string `mapstructure:"terms"`      // match ANY of these
	Categories []string `mapstructure:"categories"` // optional: restrict to these arXiv categories
	Exclude    []string `mapstructure:"exclude"`    // optional: drop results matching any of these
}

var (
	plainTermRe = regexp.MustCompile(`^[A-Za-z0-9]+$`)
	categoryRe  = regexp.MustCompile(`^[A-Za-z-]+(\.([A-Za-z-]+|\*))?$`)
)

// arxivTerm renders one term for field: plain words as-is, anything else as a quoted phrase.
func arxivTerm(field, term string) string {
	if plainTermRe.MatchString(term) {
		return field + ":" + term
	}
	return field + ":%22" + url.QueryEscape(term) + "%22"
}

// buildArxivQuery returns the query-API URL for a search.
func buildArxivQuery(s ArxivSearch) (string, error) {
	field := strings.ToLower(strings.TrimSpace(s.Field))
	switch field {
	case "":
		field = "all"
	case "all", "ti", "abs":
	default:
		return "", fmt.Errorf("field must be all, ti or abs (got %q)", s.Field)
	}

	var terms []string
	for _, t := range s.Terms {
		if t = strings.TrimSpace(t); t != "" {
			terms = append(terms, arxivTerm(field, t))
		}
	}
	if len(terms) == 0 {
		return "", fmt.Errorf("no terms")
	}
	q := strings.Join(terms, "+OR+")
	if len(terms) > 1 && (len(s.Categories) > 0 || len(s.Exclude) > 0) {
		q = "%28" + q + "%29"
	}

	var cats []string
	for _, c := range s.Categories {
		if c = strings.TrimSpace(c); c == "" {
			continue
		}
		if !categoryRe.MatchString(c) {
			return "", fmt.Errorf("bad category %q (expected something like cs.RO or cs.*)", c)
		}
		cats = append(cats, "cat:"+c)
	}
	if len(cats) > 0 {
		q += "+AND+%28" + strings.Join(cats, "+OR+") + "%29"
	}
	for _, e := range s.Exclude {
		if e = strings.TrimSpace(e); e != "" {
			q += "+ANDNOT+" + arxivTerm(field, e)
		}
	}

	limit := s.Limit
	return fmt.Sprintf("https://export.arxiv.org/api/query?search_query=%s&sortBy=submittedDate&sortOrder=descending&max_results=%d", q, limit), nil
}

// loadArxivSearches expands `arxiv_searches` into feeds (use: summary|feed) and
// analyst feed specs ("URL N", use: analyst).
func loadArxivSearches() (feeds []RSS, analyst []string) {
	var searches []ArxivSearch
	if err := viper.UnmarshalKey("arxiv_searches", &searches); err != nil {
		log.Printf("arxiv_searches: %v", err)
		return nil, nil
	}
	for i, s := range searches {
		name := s.Name
		if name == "" {
			name = fmt.Sprintf("#%d", i+1)
		}
		use := strings.ToLower(strings.TrimSpace(s.Use))
		if use == "" {
			use = "analyst"
		}
		if s.Limit <= 0 {
			s.Limit = map[string]int{"summary": 5, "analyst": 30, "feed": 20}[use]
		}
		u, err := buildArxivQuery(s)
		if err != nil {
			log.Printf("arxiv_searches %s: %v (skipped)", name, err)
			continue
		}
		switch use {
		case "summary":
			feeds = append(feeds, RSS{url: u, limit: s.Limit, summarize: true, name: "arXiv: " + name})
		case "feed":
			feeds = append(feeds, RSS{url: u, limit: s.Limit, name: "arXiv: " + name})
		case "analyst":
			analyst = append(analyst, fmt.Sprintf("%s %d", u, s.Limit))
		default:
			log.Printf("arxiv_searches %s: use must be summary, analyst or feed (got %q) (skipped)", name, s.Use)
		}
	}
	return feeds, analyst
}
