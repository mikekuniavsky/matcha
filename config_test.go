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
	for _, key := range []string{"summary_feeds", "feeds", "analyst_feeds"} {
		if len(v.GetStringSlice(key)) == 0 {
			t.Errorf("%s is empty", key)
		}
	}
	for _, f := range v.GetStringSlice("summary_feeds") {
		if u, _ := getFeedAndLimit(f); !strings.HasPrefix(u, "https://export.arxiv.org/api/query?") {
			t.Errorf("unexpected summary feed %q", f)
		}
	}
	if !v.GetBool("analyst_read_papers") || !strings.Contains(v.GetString("analyst_prompt"), "Build123d") {
		t.Error("analyst settings not parsed")
	}
}
