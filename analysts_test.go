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

func loadAnalystsFrom(t *testing.T, yaml string) ([]AnalystConfig, []string) {
	t.Helper()
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.SetConfigType("yaml")
	if err := viper.ReadConfig(strings.NewReader(yaml)); err != nil {
		t.Fatal(err)
	}
	return loadAnalysts()
}

func TestDefaultConfigHasDemandAnalyst(t *testing.T) {
	analysts, warnings := loadAnalystsFrom(t, config)
	if len(warnings) > 0 || len(analysts) != 1 {
		t.Fatalf("got %d analysts, warnings %v", len(analysts), warnings)
	}
	a := analysts[0]
	if a.Name != "Creator manufacturing demand" || !strings.Contains(a.Prompt, "PAIN") || !strings.Contains(a.Prompt, "COUNTER") || a.MaxChars != 600 {
		t.Errorf("unexpected analyst: %+v", a)
	}
	if len(a.Feeds) == 0 || len(a.GoogleNews) == 0 || len(a.allFeeds()) != len(a.Feeds)+len(a.GoogleNews) {
		t.Errorf("feeds: %d, google_news: %d, all: %d", len(a.Feeds), len(a.GoogleNews), len(a.allFeeds()))
	}
	for _, spec := range a.allFeeds() {
		u, limit := getFeedAndLimit(spec)
		if !strings.HasPrefix(u, "https://") || limit <= 0 || strings.Contains(u, " ") {
			t.Errorf("bad feed spec %q", spec)
		}
	}
	if !strings.Contains(a.allFeeds()[len(a.allFeeds())-1], "Maker%27s+Row") {
		t.Errorf("YAML quote escape lost: %s", a.allFeeds()[len(a.allFeeds())-1])
	}
}

func TestLoadAnalystsSkipsBadEntries(t *testing.T) {
	analysts, warnings := loadAnalystsFrom(t, `
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
	if len(analysts) != 1 || analysts[0].Name != "Good" || len(warnings) != 4 {
		t.Fatalf("analysts %+v, warnings %v", analysts, warnings)
	}
	job := analysts[0].job(&Config{})
	if job.tag != "#analyst:good" || job.heading != "Good" || job.maxChars != defaultAnalystMaxChars {
		t.Errorf("job: %+v", job)
	}
	want := "https://news.google.com/rss/search?hl=en-US&gl=US&ceid=US%3Aen&q=%22small+batch%22+manufacturing 10"
	if len(job.feeds) != 1 || job.feeds[0] != want {
		t.Errorf("feeds %v, want %s", job.feeds, want)
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
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"- [1] PAIN 3: \"quotes are all over the place\". Why it matters: first-hand MOQ pain."}}]}`))
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
		!strings.Contains(w.out, "**[Factory wants a 5,000 unit MOQ for my first product](https://r.example/post1)** PAIN 3") {
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
