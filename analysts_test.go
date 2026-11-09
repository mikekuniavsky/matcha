package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mmcdole/gofeed"
	"github.com/spf13/viper"
)

// loadAnalystsFrom loads analysts (with arXiv analyst searches attached) from a YAML config.
func loadAnalystsFrom(t *testing.T, yaml string) (*Config, []string) {
	t.Helper()
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.SetConfigType("yaml")
	if err := viper.ReadConfig(strings.NewReader(yaml)); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{AnalystFeeds: viper.GetStringSlice("analyst_feeds"), AnalystPrompt: viper.GetString("analyst_prompt")}
	_, searches := loadArxivSearches()
	return cfg, loadAnalysts(cfg, searches)
}

func TestDefaultConfigAnalysts(t *testing.T) {
	cfg, warnings := loadAnalystsFrom(t, config)
	list := cfg.analystList()
	if len(warnings) > 0 || len(list) != 2 {
		t.Fatalf("got %d analysts, warnings %v", len(list), warnings)
	}
	cad, demand := list[0], list[1]
	if cad.Name != "CAD research scout" || !cad.ReadPapers || cad.MaxPapers != 3 || !strings.Contains(cad.Prompt, "Build123d") {
		t.Errorf("CAD analyst: %+v", cad)
	}
	if len(cad.Feeds) != 1 || !strings.HasPrefix(cad.Feeds[0], "https://export.arxiv.org/api/query?") || !strings.HasSuffix(cad.Feeds[0], " 30") {
		t.Errorf("Broad CAD scan not attached to the CAD analyst: %v", cad.Feeds)
	}
	if job := cad.job(true); job.tag != "#analyst:cad-research-scout" || !job.readPapers || !job.notify || job.maxChars != defaultAnalystMaxChars {
		t.Errorf("CAD job: %+v", job)
	}
	if demand.Name != "Creator manufacturing demand" || !strings.Contains(demand.Prompt, "minimum order quantities") ||
		!strings.Contains(demand.Prompt, "evidence against") || demand.MaxChars != 600 || demand.ReadPapers {
		t.Errorf("demand analyst: %+v", demand)
	}
	if len(demand.Feeds) == 0 || len(demand.GoogleNews) == 0 || len(demand.allFeeds()) != len(demand.Feeds)+len(demand.GoogleNews) {
		t.Errorf("feeds: %d, google_news: %d, all: %d", len(demand.Feeds), len(demand.GoogleNews), len(demand.allFeeds()))
	}
	for _, spec := range demand.allFeeds() {
		u, limit := getFeedAndLimit(spec)
		if !strings.HasPrefix(u, "https://") || limit <= 0 || strings.Contains(u, " ") {
			t.Errorf("bad feed spec %q", spec)
		}
	}
	if !strings.Contains(demand.allFeeds()[len(demand.allFeeds())-1], "Maker%27s+Row") {
		t.Errorf("YAML quote escape lost: %s", demand.allFeeds()[len(demand.allFeeds())-1])
	}
	if job := demand.job(false); job.notify || job.readPapers {
		t.Errorf("demand job: %+v", job)
	}
}

func TestLoadAnalystsSkipsBadEntries(t *testing.T) {
	cfg, warnings := loadAnalystsFrom(t, `
analysts:
  - name: Good
    prompt: pick
    google_news: ['"small batch" manufacturing']
  - prompt: no name
    feeds: [https://a.example]
  - name: No prompt
    feeds: [https://a.example]
  - name: No feeds
    prompt: pick
  - name: good
    prompt: duplicate
    feeds: [https://a.example]
`)
	if len(cfg.Analysts) != 1 || cfg.Analysts[0].Name != "Good" || len(warnings) != 4 {
		t.Fatalf("analysts %+v, warnings %v", cfg.Analysts, warnings)
	}
	job := cfg.Analysts[0].job(false)
	if job.tag != "#analyst:good" || job.heading != "Good" || job.maxChars != defaultAnalystMaxChars || job.readPapers {
		t.Errorf("job: %+v", job)
	}
	want := "https://news.google.com/rss/search?hl=en-US&gl=US&ceid=US%3Aen&q=%22small+batch%22+manufacturing 10"
	if len(job.feeds) != 1 || job.feeds[0] != want {
		t.Errorf("feeds %v, want %s", job.feeds, want)
	}
}

func TestArxivAnalystSearchesFindTheirAnalyst(t *testing.T) {
	cfg, warnings := loadAnalystsFrom(t, `
arxiv_searches:
  - {name: default, use: analyst, terms: [CAD]}
  - {name: named, use: analyst, analyst: second, terms: [OpenSCAD]}
  - {name: typo, use: analyst, analyst: nobody, terms: [x]}
analysts:
  - {name: First, prompt: a}
  - {name: Second, prompt: b, feeds: [https://b.example]}
`)
	if len(warnings) != 1 || !strings.Contains(warnings[0], `no analyst named "nobody"`) {
		t.Errorf("warnings %v", warnings)
	}
	if len(cfg.Analysts) != 2 || len(cfg.Analysts[0].Feeds) != 1 || !strings.Contains(cfg.Analysts[0].Feeds[0], "all:CAD") ||
		len(cfg.Analysts[1].Feeds) != 2 || !strings.Contains(cfg.Analysts[1].Feeds[1], "all:OpenSCAD") {
		t.Errorf("analysts %+v", cfg.Analysts)
	}
}

// Old configs keep working: analyst_prompt plus arXiv searches with no analyst_feeds becomes a
// "Daily Analysis" analyst that runs first, keeps the old bookkeeping and gets the searches.
func TestLegacyAnalystKeysStillWork(t *testing.T) {
	cfg, warnings := loadAnalystsFrom(t, `
analyst_prompt: pick CAD
arxiv_searches:
  - {name: scan, use: analyst, terms: [CAD]}
analysts:
  - {name: Demand, prompt: pick demand, feeds: [https://d.example]}
  - {name: daily analysis, prompt: clash, feeds: [https://x.example]}
`)
	list := cfg.analystList()
	if len(warnings) != 1 || len(list) != 2 || list[0].Name != "Daily Analysis" || list[1].Name != "Demand" {
		t.Fatalf("list %+v, warnings %v", list, warnings)
	}
	if len(list[0].Feeds) != 1 || len(list[1].Feeds) != 1 {
		t.Errorf("arXiv search went to the wrong analyst: %+v", list)
	}
	if job := list[0].job(true); job.tag != analystTag || job.maxChars != 0 || job.prompt != "pick CAD" {
		t.Errorf("legacy job: %+v", job)
	}
}

func TestAnalystTextUsesContentStripsHTMLAndCaps(t *testing.T) {
	item := &gofeed.Item{Description: "  ", Content: "<div><p>We need a factory &amp; our MOQ is 5,000.</p><p>Help!</p></div>"}
	if got := analystText(item, 0); got != "We need a factory & our MOQ is 5,000. Help!" {
		t.Errorf("got %q", got)
	}
	if got := analystText(item, 17); got != "We need a factory…" {
		t.Errorf("capped: got %q", got)
	}
	if got := analystText(&gofeed.Item{Description: "Abstract text", Content: "ignored"}, 0); got != "Abstract text" {
		t.Errorf("description first: got %q", got)
	}
}

// A named analyst runs without the main Analyst, sees the post body from an Atom feed (Reddit
// puts it only in <content>), uses its own prompt and model, gets its own section, and is not
// hidden by the main Analyst's bookkeeping for the same link.
func TestNamedAnalystRunsWithItsOwnPromptAndSection(t *testing.T) {
	const atom = `<?xml version="1.0" encoding="UTF-8"?><feed xmlns="http://www.w3.org/2005/Atom"><title>r/smallbusiness</title>
<entry><title>Factory wants a 5,000 unit MOQ for my first product</title><link href="https://r.example/post1"/>
<content type="html">&lt;p&gt;I have 300k followers and quotes are all over the place.&lt;/p&gt;</content></entry></feed>`
	src := feedServer(t, atom)

	var req struct {
		Model    string                     `json:"model"`
		Messages []struct{ Content string } `json:"messages"`
	}
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"- [1] Poster says \"quotes are all over the place\". Why it matters: first-hand MOQ pain."}}]}`))
	}))
	t.Cleanup(llm.Close)

	store := testStore(t)
	if err := store.MarkAsSeen("https://r.example/post1"+analystTag, "", "t", "f", "l"); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{ShowEmptySections: true, OpenAIBaseURL: llm.URL, AnalystModel: "main-model",
		Analysts: []AnalystConfig{{Name: "Creator demand", Prompt: "Find manufacturing pain.", Model: "demand-model", Feeds: []string{src.URL}}}}
	w := &captureWriter{}
	RunAnalyst(cfg, store, NewLLMClient(cfg), w, gofeed.NewParser())

	if req.Model != "demand-model" || len(req.Messages) != 1 {
		t.Fatalf("request: %+v", req)
	}
	prompt := req.Messages[0].Content
	if !strings.HasPrefix(prompt, "Find manufacturing pain.") || !strings.Contains(prompt, "[1] Factory wants a 5,000 unit MOQ for my first product: I have 300k followers") || strings.Contains(prompt, "SELECTED") {
		t.Errorf("prompt:\n%s", prompt)
	}
	if strings.Contains(w.out, "Daily Analysis") || !strings.Contains(w.out, "## Creator demand:") ||
		!strings.Contains(w.out, "**[Factory wants a 5,000 unit MOQ for my first product](https://r.example/post1)** Poster says") {
		t.Errorf("output:\n%s", w.out)
	}

	// On a later day the item counts as already screened by this analyst (same-day reruns screen again).
	if _, err := store.db.Exec("UPDATE seen SET date = '2000-01-01'"); err != nil {
		t.Fatal(err)
	}
	w = &captureWriter{}
	RunAnalyst(cfg, store, NewLLMClient(cfg), w, gofeed.NewParser())
	if !strings.Contains(w.out, "## Creator demand:") || !strings.Contains(w.out, "1 already analyzed before") {
		t.Errorf("second run:\n%s", w.out)
	}
}
