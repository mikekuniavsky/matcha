package main

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/spf13/viper"
)

// AnalystConfig is one entry of `analysts` in config.yaml: a named Analyst with its own feeds and
// prompt, written as its own section. It runs after the main Analyst (analyst_feeds/analyst_prompt).
type AnalystConfig struct {
	Name       string   `mapstructure:"name"`        // section title and bookkeeping key
	Prompt     string   `mapstructure:"prompt"`      // what to pick; the numbered output format is added for you
	Model      string   `mapstructure:"model"`       // optional; defaults to analyst_model
	Feeds      []string `mapstructure:"feeds"`       // "URL N", like analyst_feeds
	GoogleNews []string `mapstructure:"google_news"` // optional Google News search queries, first 10 results each
	MaxChars   int      `mapstructure:"max_chars"`   // per-item text cap (default 600); 0 or less = default
}

const (
	defaultAnalystMaxChars = 600
	analystGoogleNewsLimit = 10
)

// googleNewsSearchURL is the RSS feed for one Google News search query.
func googleNewsSearchURL(query string) string {
	return "https://news.google.com/rss/search?hl=en-US&gl=US&ceid=US%3Aen&q=" + url.QueryEscape(strings.TrimSpace(query))
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// allFeeds returns the analyst's feeds plus its Google News searches, in "URL N" form.
func (a AnalystConfig) allFeeds() []string {
	feeds := append([]string{}, a.Feeds...)
	for _, q := range a.GoogleNews {
		if strings.TrimSpace(q) != "" {
			feeds = append(feeds, fmt.Sprintf("%s %d", googleNewsSearchURL(q), analystGoogleNewsLimit))
		}
	}
	return feeds
}

func (a AnalystConfig) job(cfg *Config) analystJob {
	maxChars := a.MaxChars
	if maxChars <= 0 {
		maxChars = defaultAnalystMaxChars
	}
	return analystJob{
		heading:  a.Name,
		feeds:    a.allFeeds(),
		prompt:   a.Prompt,
		model:    a.Model,
		tag:      analystTag + ":" + strings.Trim(slugRe.ReplaceAllString(strings.ToLower(a.Name), "-"), "-"),
		maxChars: maxChars,
	}
}

// loadAnalysts reads `analysts`, skipping (with a warning) entries without a name, prompt or feeds.
func loadAnalysts() ([]AnalystConfig, []string) {
	var raw []AnalystConfig
	if err := viper.UnmarshalKey("analysts", &raw); err != nil {
		return nil, []string{fmt.Sprintf("analysts: %v", err)}
	}
	var out []AnalystConfig
	var warnings []string
	names := map[string]bool{}
	for i, a := range raw {
		a.Name = strings.TrimSpace(a.Name)
		switch {
		case a.Name == "":
			warnings = append(warnings, fmt.Sprintf("analysts entry %d has no name; skipped", i+1))
		case strings.TrimSpace(a.Prompt) == "":
			warnings = append(warnings, fmt.Sprintf("analyst %q has no prompt; skipped", a.Name))
		case len(a.allFeeds()) == 0:
			warnings = append(warnings, fmt.Sprintf("analyst %q has no feeds or google_news queries; skipped", a.Name))
		case names[strings.ToLower(a.Name)]:
			warnings = append(warnings, fmt.Sprintf("analyst %q is listed twice; the second is skipped", a.Name))
		default:
			names[strings.ToLower(a.Name)] = true
			out = append(out, a)
		}
	}
	return out, warnings
}
