package main

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// The default config is written verbatim to disk on first run, so it must be valid YAML.
func TestDefaultConfigParses(t *testing.T) {
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader(config)); err != nil {
		t.Fatal(err)
	}
	if len(v.GetStringSlice("feeds")) == 0 {
		t.Error("feeds is empty")
	}
	var searches []ArxivSearch
	if err := v.UnmarshalKey("arxiv_searches", &searches); err != nil || len(searches) != 2 {
		t.Fatalf("arxiv_searches: %v (%d entries)", err, len(searches))
	}
	for _, s := range searches {
		if u, err := buildArxivQuery(s); err != nil || !strings.HasPrefix(u, "https://export.arxiv.org/api/query?search_query=") {
			t.Errorf("search %q: %q, %v", s.Name, u, err)
		}
	}
	var watches []CitationWatch
	if err := v.UnmarshalKey("citation_watch", &watches); err != nil || len(watches) != 1 || len(watches[0].Papers) != 6 {
		t.Fatalf("citation_watch: %v (%+v)", err, watches)
	}
	for _, p := range watches[0].Papers {
		if _, err := refFilter(p); err != nil {
			t.Errorf("citation_watch paper %q: %v", p, err)
		}
	}
	if v.IsSet("analyst_prompt") || v.IsSet("analyst_feeds") {
		t.Error("default config should use analysts:, not the old analyst_* keys")
	}
}
