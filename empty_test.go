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
