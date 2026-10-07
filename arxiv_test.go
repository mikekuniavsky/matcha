package main

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestBuildArxivQuery(t *testing.T) {
	got, err := buildArxivQuery(ArxivSearch{Limit: 7, Terms: []string{"CadQuery", "text-to-CAD", "CAD generation"}})
	want := "https://export.arxiv.org/api/query?search_query=all:CadQuery+OR+all:%22text-to-CAD%22+OR+all:%22CAD+generation%22&sortBy=submittedDate&sortOrder=descending&max_results=7"
	if err != nil || got != want {
		t.Fatalf("got  %q, %v\nwant %q", got, err, want)
	}

	got, err = buildArxivQuery(ArxivSearch{Limit: 20, Field: "TI", Terms: []string{"CAD", "B-rep"}, Categories: []string{"cs.RO", "cs.*"}, Exclude: []string{"computer-aided diagnosis"}})
	want = "https://export.arxiv.org/api/query?search_query=%28ti:CAD+OR+ti:%22B-rep%22%29+AND+%28cat:cs.RO+OR+cat:cs.*%29+ANDNOT+ti:%22computer-aided+diagnosis%22&sortBy=submittedDate&sortOrder=descending&max_results=20"
	if err != nil || got != want {
		t.Fatalf("got  %q, %v\nwant %q", got, err, want)
	}
}

func TestBuildArxivQueryErrors(t *testing.T) {
	for name, s := range map[string]ArxivSearch{
		"no terms":     {Terms: []string{" ", ""}},
		"bad field":    {Field: "title", Terms: []string{"x"}},
		"bad category": {Terms: []string{"x"}, Categories: []string{"cs.RO OR all:x"}},
	} {
		if _, err := buildArxivQuery(s); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestLoadArxivSearches(t *testing.T) {
	viper.Reset()
	defer viper.Reset()
	viper.SetConfigType("yaml")
	err := viper.ReadConfig(strings.NewReader(`
arxiv_searches:
  - name: a
    use: summary
    terms: [CadQuery]
  - name: b
    use: analyst
    limit: 12
    terms: [CAD]
  - name: c
    use: feed
    terms: [OpenSCAD]
  - name: bad
    use: nonsense
    terms: [x]
`))
	if err != nil {
		t.Fatal(err)
	}
	feeds, analyst := loadArxivSearches()
	if len(feeds) != 2 || !feeds[0].summarize || feeds[0].limit != 5 || feeds[1].summarize || feeds[1].limit != 20 {
		t.Errorf("feeds = %+v", feeds)
	}
	if len(analyst) != 1 || !strings.HasSuffix(analyst[0], " 12") {
		t.Errorf("analyst = %v", analyst)
	}
}
