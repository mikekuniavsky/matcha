package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mmcdole/gofeed"
)

func testStore(t *testing.T) *Storage {
	t.Helper()
	store, err := NewStorage(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	return store
}

func feedServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
	t.Cleanup(srv.Close)
	return srv
}

func runFeed(t *testing.T, rss RSS, cfg *Config, store *Storage) string {
	t.Helper()
	shownThisRun = map[string]bool{}
	w := &captureWriter{}
	ProcessFeed(rss, cfg, store, nil, w, gofeed.NewParser())
	return w.out
}

const emptyRSS = `<?xml version="1.0"?><rss version="2.0"><channel><title>Empty Feed</title><link>https://e.example</link></channel></rss>`

func TestEmptyFeedGetsANote(t *testing.T) {
	srv := feedServer(t, emptyRSS)
	cfg := &Config{ShowEmptySections: true}
	out := runFeed(t, RSS{url: srv.URL, limit: 10}, cfg, testStore(t))
	if !strings.Contains(out, "HEADER[Empty Feed]") || !strings.Contains(out, "No items returned by this feed.") {
		t.Errorf("missing note:\n%s", out)
	}
	if strings.Contains(out, "weekends") {
		t.Errorf("arXiv hint shown for a non-arXiv feed:\n%s", out)
	}

	// The arXiv RSS hint only appears for rss.arxiv.org feeds.
	if note := emptyFeedNote("https://rss.arxiv.org/rss/cs.GR", 0, 0, 0); !strings.Contains(note, "weekends") {
		t.Errorf("arXiv RSS note: %q", note)
	}
	if note := emptyFeedNote("https://export.arxiv.org/api/query?search_query=x", 0, 0, 0); strings.Contains(note, "weekends") {
		t.Errorf("arXiv API feeds are not weekend-limited: %q", note)
	}
}

func TestAllSeenFeedGetsANote(t *testing.T) {
	srv := feedServer(t, `<?xml version="1.0"?><rss version="2.0"><channel><title>Old News</title><link>https://o.example</link>
<item><title>One</title><link>https://o.example/1</link></item><item><title>Two</title><link>https://o.example/2</link></item></channel></rss>`)
	store := testStore(t)
	for _, l := range []string{"https://o.example/1", "https://o.example/2"} {
		if _, err := store.db.Exec("INSERT INTO seen(url, date, summary, title, feed_title) values(?,?,?,?,?)", l, "2000-01-01", "", "t", "Old News"); err != nil {
			t.Fatal(err)
		}
	}
	out := runFeed(t, RSS{url: srv.URL, limit: 10, name: "My Section"}, &Config{ShowEmptySections: true}, store)
	if !strings.Contains(out, "HEADER[My Section]") || !strings.Contains(out, "No new items: 2 read, 2 already seen on earlier days.") {
		t.Errorf("unexpected output:\n%s", out)
	}
}

func TestUnreadableFeedGetsANote(t *testing.T) {
	srv := feedServer(t, "")
	srv.Close() // connection refused
	out := runFeed(t, RSS{url: srv.URL + "/feed.xml", limit: 10, name: "arXiv: CAD tools"}, &Config{ShowEmptySections: true}, testStore(t))
	if !strings.Contains(out, "HEADER[arXiv: CAD tools]") || !strings.Contains(out, "Could not read this feed") {
		t.Errorf("unexpected output:\n%s", out)
	}
}

func TestEmptyNotesCanBeTurnedOff(t *testing.T) {
	srv := feedServer(t, emptyRSS)
	if out := runFeed(t, RSS{url: srv.URL, limit: 10}, &Config{ShowEmptySections: false}, testStore(t)); out != "" {
		t.Errorf("expected no output, got:\n%s", out)
	}
}

func TestAnalystNoNewArticlesNote(t *testing.T) {
	srv := feedServer(t, emptyRSS)
	cfg := &Config{ShowEmptySections: true, AnalystFeeds: []string{srv.URL, "http://127.0.0.1:1/none"}, AnalystPrompt: "pick"}
	w := &captureWriter{}
	RunAnalyst(cfg, testStore(t), NewLLMClient(cfg), w, gofeed.NewParser())
	if !strings.Contains(w.out, "Daily Analysis") || !strings.Contains(w.out, "No new articles to screen: 0 read from 1 feeds") || !strings.Contains(w.out, "1 feeds could not be read") {
		t.Errorf("unexpected analyst output:\n%s", w.out)
	}
}

func TestSectionWithResultsSaysWhatWasSkipped(t *testing.T) {
	srv := feedServer(t, `<?xml version="1.0"?><rss version="2.0"><channel><title>Mixed</title><link>https://m.example</link>
<item><title>Fresh story</title><link>https://m.example/new</link></item><item><title>Old story</title><link>https://m.example/old</link></item></channel></rss>`)
	store := testStore(t)
	if _, err := store.db.Exec("INSERT INTO seen(url, date, summary, title, feed_title) values(?,?,?,?,?)", "https://m.example/old", "2000-01-01", "", "t", "Mixed"); err != nil {
		t.Fatal(err)
	}
	out := runFeed(t, RSS{url: srv.URL, limit: 10}, &Config{ShowEmptySections: true}, store)
	if !strings.Contains(out, "LINK[Fresh story]") || strings.Contains(out, "Old story") {
		t.Errorf("wrong items listed:\n%s", out)
	}
	if !strings.Contains(out, "_Not shown again: 1 already seen on earlier days_") {
		t.Errorf("missing skipped-items footnote:\n%s", out)
	}
}

func TestBuildGoogleNewsQuery(t *testing.T) {
	for in, want := range map[string]string{
		"CadQuery,OpenSCAD":                               "CadQuery | OpenSCAD",
		"George Hotz, ChatGPT ,Copenhagen":                "George Hotz | ChatGPT | Copenhagen",
		`"text-to-CAD","AI CAD", -loonie -forex -USD/CAD`: `("text-to-CAD" | "AI CAD") -loonie -forex -USD/CAD`,
		"OpenSCAD,-forex":                                 "OpenSCAD -forex",
		"":                                                "",
	} {
		if got := buildGoogleNewsQuery(in); got != want {
			t.Errorf("buildGoogleNewsQuery(%q) = %q, want %q", in, got, want)
		}
	}
}

const oneItemRSS = `<?xml version="1.0"?><rss version="2.0"><channel><title>Papers</title><link>https://p.example</link>
<item><title>Text-to-CAD with agents</title><link>https://p.example/1</link><description>CadQuery code</description></item></channel></rss>`

func TestAnalystNoteCountsEmptyFeeds(t *testing.T) {
	empty, seen := feedServer(t, emptyRSS), feedServer(t, oneItemRSS)
	store := testStore(t)
	if _, err := store.db.Exec("INSERT INTO seen(url, date, summary, title, feed_title) values(?,?,?,?,?)", "https://p.example/1"+analystTag, "2000-01-01", "", "t", "Papers"); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{ShowEmptySections: true, AnalystFeeds: []string{empty.URL, seen.URL, empty.URL}, AnalystPrompt: "pick"}
	w := &captureWriter{}
	RunAnalyst(cfg, store, NewLLMClient(cfg), w, gofeed.NewParser())
	if !strings.Contains(w.out, "1 read from 3 feeds, 1 already analyzed before; ⚠️ 2 of 3 feeds returned no items") || strings.Contains(w.out, "could not be read") {
		t.Errorf("unexpected analyst output:\n%s", w.out)
	}
}

func TestAnalysisNotesEmptyFeedsAndDropsLeakedReasoning(t *testing.T) {
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"- [1] Draft pick. Why it matters: draft.\n6. Final polish.<channel|>- [1] Agents write CadQuery. Why it matters: direct fit.\nSELECTED: none"}}]}`))
	}))
	t.Cleanup(llm.Close)
	empty, items := feedServer(t, emptyRSS), feedServer(t, oneItemRSS)
	cfg := &Config{ShowEmptySections: true, AnalystFeeds: []string{empty.URL, items.URL}, AnalystPrompt: "pick",
		OpenAIBaseURL: llm.URL, AnalystReadPapers: true, AnalystMaxPapers: 3}
	w := &captureWriter{}
	RunAnalyst(cfg, testStore(t), NewLLMClient(cfg), w, gofeed.NewParser())
	if strings.Contains(w.out, "Draft pick") || strings.Contains(w.out, "<channel|>") || !strings.Contains(w.out, "Agents write CadQuery") {
		t.Errorf("leaked reasoning not removed:\n%s", w.out)
	}
	if !strings.Contains(w.out, "1 of 2 analyst feeds returned no items") {
		t.Errorf("missing empty-feed line:\n%s", w.out)
	}
}

func TestStripReasoning(t *testing.T) {
	for in, want := range map[string]string{
		"plain answer":                        "plain answer",
		"<think>hmm</think>\n\nanswer":        "answer",
		"<|channel>thought\nx<channel|>final": "final",
		"draft<channel|>a<channel|> last ":    "last",
	} {
		if got := stripReasoning(in); got != want {
			t.Errorf("stripReasoning(%q) = %q, want %q", in, got, want)
		}
	}
}
