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
	if !v.GetBool("analyst_read_papers") || !strings.Contains(v.GetString("analyst_prompt"), "Build123d") {
		t.Error("analyst settings not parsed")
	}
}
