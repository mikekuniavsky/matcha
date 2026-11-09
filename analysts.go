package main

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/spf13/viper"
)

// AnalystConfig is one entry of `analysts` in config.yaml: an Analyst with its own feeds, prompt and
// section in the digest. The old single-Analyst keys (analyst_feeds, analyst_prompt, ...) still work
// and become an analyst named "Daily Analysis" that runs first (see legacyAnalyst).
type AnalystConfig struct {
	Name       string   `mapstructure:"name"`        // section title and bookkeeping key
	Prompt     string   `mapstructure:"prompt"`      // what to pick; the numbered output format is added for you
	Model      string   `mapstructure:"model"`       // optional; defaults to openai_model, then gpt-4o
	Feeds      []string `mapstructure:"feeds"`       // "URL N", default 20 items
	GoogleNews []string `mapstructure:"google_news"` // optional Google News search queries, first 10 results each
	MaxChars   int      `mapstructure:"max_chars"`   // per-item text cap (default 1500); 0 or less = default
	ReadPapers bool     `mapstructure:"read_papers"` // pick arXiv papers for full-text summaries (MinerU)
	MaxPapers  int      `mapstructure:"max_papers"`  // at most this many picks a run (default 3)

	legacy bool // built from the analyst_* keys: keeps the "#analyst" bookkeeping and no text cap
}

const (
	defaultAnalystMaxChars  = 1500 // fits an arXiv abstract; long forum posts get cut
	defaultAnalystMaxPapers = 3
	legacyAnalystName       = "Daily Analysis"
	analystGoogleNewsLimit  = 10
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

func (a AnalystConfig) job(first bool) analystJob {
	maxChars := a.MaxChars
	if maxChars <= 0 && !a.legacy {
		maxChars = defaultAnalystMaxChars
	}
	maxPapers := a.MaxPapers
	if maxPapers <= 0 {
		maxPapers = defaultAnalystMaxPapers
	}
	tag := analystTag + ":" + strings.Trim(slugRe.ReplaceAllString(strings.ToLower(a.Name), "-"), "-")
	if a.legacy {
		tag = analystTag
	}
	return analystJob{
		heading:    a.Name,
		feeds:      a.allFeeds(),
		prompt:     a.Prompt,
		model:      a.Model,
		tag:        tag,
		maxChars:   maxChars,
		readPapers: a.ReadPapers,
		maxPapers:  maxPapers,
		notify:     first, // notification_trigger applies to the first analyst
	}
}

// legacyAnalyst turns the old single-Analyst keys into an analyst, or returns false when they are unused.
func (cfg *Config) legacyAnalyst() (AnalystConfig, bool) {
	if len(cfg.AnalystFeeds) == 0 || strings.TrimSpace(cfg.AnalystPrompt) == "" {
		return AnalystConfig{}, false
	}
	max := cfg.AnalystMaxPapers
	return AnalystConfig{Name: legacyAnalystName, Prompt: cfg.AnalystPrompt, Model: cfg.AnalystModel, Feeds: cfg.AnalystFeeds,
		ReadPapers: cfg.AnalystReadPapers && max > 0, MaxPapers: max, legacy: true}, true
}

// analystList is every analyst to run, in order: the legacy one (if configured), then `analysts`.
func (cfg *Config) analystList() []AnalystConfig {
	var list []AnalystConfig
	if a, ok := cfg.legacyAnalyst(); ok {
		list = append(list, a)
	}
	return append(list, cfg.Analysts...)
}

// readAnalysts reads the raw `analysts` list.
func readAnalysts() ([]AnalystConfig, error) {
	var raw []AnalystConfig
	err := viper.UnmarshalKey("analysts", &raw)
	for i := range raw {
		raw[i].Name = strings.TrimSpace(raw[i].Name)
	}
	return raw, err
}

// attachArxivAnalystFeeds adds `arxiv_searches` with `use: analyst` to the analyst they name (`analyst:`),
// or, when they name none, to the legacy analyst_feeds if those are in use, else to the first analyst.
func attachArxivAnalystFeeds(cfg *Config, raw []AnalystConfig, searches []arxivAnalystFeed) ([]AnalystConfig, []string) {
	var warnings []string
	_, legacy := cfg.legacyAnalyst()
	if !legacy && cfg.AnalystPrompt != "" {
		legacy = true // analyst_prompt with only arXiv feeds is still the legacy setup
	}
	for _, s := range searches {
		target := strings.ToLower(strings.TrimSpace(s.analyst))
		if target == "" && legacy {
			cfg.AnalystFeeds = append(cfg.AnalystFeeds, s.spec)
			continue
		}
		found := false
		for i := range raw {
			if target == "" || strings.ToLower(raw[i].Name) == target {
				raw[i].Feeds = append(raw[i].Feeds, s.spec)
				found = true
				break
			}
		}
		if !found {
			if target == "" {
				warnings = append(warnings, fmt.Sprintf("arxiv_searches %s: use: analyst, but no analyst is configured (skipped)", s.name))
			} else {
				warnings = append(warnings, fmt.Sprintf("arxiv_searches %s: no analyst named %q (skipped)", s.name, s.analyst))
			}
		}
	}
	return raw, warnings
}

// validateAnalysts drops (with a warning) entries without a name, prompt or feeds, and repeated names.
func validateAnalysts(raw []AnalystConfig, legacy bool) ([]AnalystConfig, []string) {
	var out []AnalystConfig
	var warnings []string
	names := map[string]bool{}
	if legacy {
		names[strings.ToLower(legacyAnalystName)] = true
	}
	for i, a := range raw {
		switch {
		case a.Name == "":
			warnings = append(warnings, fmt.Sprintf("analysts entry %d has no name; skipped", i+1))
		case strings.TrimSpace(a.Prompt) == "":
			warnings = append(warnings, fmt.Sprintf("analyst %q has no prompt; skipped", a.Name))
		case len(a.allFeeds()) == 0:
			warnings = append(warnings, fmt.Sprintf("analyst %q has no feeds, google_news queries or arxiv_searches; skipped", a.Name))
		case names[strings.ToLower(a.Name)]:
			warnings = append(warnings, fmt.Sprintf("analyst %q is listed twice (or clashes with analyst_prompt's %q); skipped", a.Name, legacyAnalystName))
		default:
			names[strings.ToLower(a.Name)] = true
			out = append(out, a)
		}
	}
	return out, warnings
}

// loadAnalysts reads `analysts`, attaches arXiv analyst searches, and validates the result.
func loadAnalysts(cfg *Config, searches []arxivAnalystFeed) []string {
	raw, err := readAnalysts()
	if err != nil {
		return []string{fmt.Sprintf("analysts: %v", err)}
	}
	raw, warnings := attachArxivAnalystFeeds(cfg, raw, searches)
	_, legacy := cfg.legacyAnalyst()
	var more []string
	cfg.Analysts, more = validateAnalysts(raw, legacy)
	return append(warnings, more...)
}
