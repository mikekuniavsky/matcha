package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mmcdole/gofeed"
	"github.com/spf13/viper"
)

const (
	oaGenCAD3D = `{"id":"https://openalex.org/W1","doi":"https://doi.org/10.48550/arxiv.2509.15246","display_name":"GenCAD-3D","publication_year":2025,
		"referenced_works":["https://openalex.org/W100","https://openalex.org/W101","https://openalex.org/W999"]}`
	oaCADCoder = `{"id":"https://openalex.org/W2","doi":"https://doi.org/10.48550/arxiv.2505.14646","display_name":"CAD-Coder","publication_year":2025,
		"referenced_works":["https://openalex.org/W100","https://openalex.org/W102"]}`
)

// fakeOpenAlex answers the few /works queries Matcha makes, in the shapes OpenAlex documents.
func fakeOpenAlex(t *testing.T) (*httptest.Server, *[]url.Values) {
	t.Helper()
	var mu sync.Mutex
	var requests []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/works" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		mu.Lock()
		requests = append(requests, q)
		mu.Unlock()
		filter := q.Get("filter")
		var results string
		switch {
		case filter == "doi:10.48550/arxiv.2509.15246":
			results = oaGenCAD3D
		case filter == "doi:10.48550/arxiv.2505.14646":
			results = oaCADCoder
		case strings.HasPrefix(filter, "cites:W1|W2,from_publication_date:"):
			results = `{"id":"https://openalex.org/W10","display_name":"Newest arXiv paper","publication_date":"2026-11-10",
				"locations":[{"landing_page_url":"http://arxiv.org/abs/2611.00001v1"}],
				"abstract_inverted_index":{"We":[0],"cite":[1],"CAD-Coder":[2],"and":[3],"extend":[4],"it":[5]}},
				{"id":"https://openalex.org/W11","display_name":"A journal paper","publication_date":"2026-11-12","doi":"https://doi.org/10.1000/xyz"},
				{"id":"https://openalex.org/W2","display_name":"CAD-Coder (a seed)","publication_date":"2026-11-13"}`
		case strings.HasPrefix(filter, "openalex:"):
			results = `{"id":"https://openalex.org/W100","display_name":"DeepCAD","publication_year":2021,"cited_by_count":500,
				"locations":[{"landing_page_url":"https://arxiv.org/abs/2105.09492"}]}`
		default:
			http.Error(w, "unexpected filter "+filter, 400)
			return
		}
		w.Write([]byte(`{"results":[` + results + `]}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &requests
}

func useFakeOpenAlex(t *testing.T, srv *httptest.Server) {
	t.Helper()
	oldBase, oldEmail, oldKey := openAlexBase, openAlexEmail, openAlexKey
	openAlexBase, openAlexEmail, openAlexKey = srv.URL, "me@example.com", ""
	resolvedWorks = map[string]oaWork{}
	t.Cleanup(func() {
		openAlexBase, openAlexEmail, openAlexKey = oldBase, oldEmail, oldKey
		resolvedWorks = map[string]oaWork{}
	})
	noSleep(t)
}

func TestCitationFeedURLRoundTrip(t *testing.T) {
	u, err := url.Parse(citationFeedURL("Text & image to CAD", []string{"arXiv:2509.15246", " 10.1115/1.4069276 "}, 14))
	if err != nil || u.Scheme+":" != citeScheme {
		t.Fatalf("parse: %v %v", u, err)
	}
	q := u.Query()
	if q.Get("name") != "Text & image to CAD" || q.Get("days") != "14" || len(q["ref"]) != 2 || q["ref"][1] != "10.1115/1.4069276" {
		t.Errorf("query = %v", q)
	}
	if strings.Contains(u.String(), " ") {
		t.Errorf("feed URL must not contain spaces (the feed list splits on them): %s", u)
	}
}

func TestRefFilter(t *testing.T) {
	for in, want := range map[string]string{
		"arXiv:2509.15246":                  "doi:10.48550/arxiv.2509.15246",
		"2509.15246v2":                      "doi:10.48550/arxiv.2509.15246",
		"https://arxiv.org/abs/2505.14646":  "doi:10.48550/arxiv.2505.14646",
		"arXiv:cs/0501001":                  "doi:10.48550/arxiv.cs/0501001",
		"10.1115/1.4069276":                 "doi:10.1115/1.4069276",
		"https://doi.org/10.1115/1.4069276": "doi:10.1115/1.4069276",
		"W2741809807":                       "openalex:W2741809807",
		"https://openalex.org/w123":         "openalex:W123",
	} {
		if got, err := refFilter(in); err != nil || got != want {
			t.Errorf("refFilter(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "GenCAD-3D", "arXiv:abc", "2509"} {
		if _, err := refFilter(bad); err == nil {
			t.Errorf("refFilter(%q) accepted", bad)
		}
	}
}

func TestOpenAlexWorkHelpers(t *testing.T) {
	w := oaWork{ID: "https://openalex.org/W7", DOI: "https://doi.org/10.48550/arXiv.2601.12641v2",
		AbstractInvertedIndex: map[string][]int{"Generating": {0}, "CAD": {1, 3}, "with": {2}, "STEP": {4}}}
	if w.shortID() != "W7" || w.arxivID() != "2601.12641" || w.link() != "https://arxiv.org/abs/2601.12641" {
		t.Errorf("id/link: %q %q %q", w.shortID(), w.arxivID(), w.link())
	}
	if w.abstract() != "Generating CAD with CAD STEP" || w.name() != "W7" {
		t.Errorf("abstract/name: %q %q", w.abstract(), w.name())
	}
	if (oaWork{ID: "https://openalex.org/W8", DOI: "https://doi.org/10.1/x"}).link() != "https://doi.org/10.1/x" {
		t.Error("DOI fallback")
	}
}

func TestFetchOpenAlexCites(t *testing.T) {
	srv, requests := fakeOpenAlex(t)
	useFakeOpenAlex(t, srv)

	feed, res, err := fetchFeed(gofeed.NewParser(), citationFeedURL("Code generation", []string{"arXiv:2509.15246", "arXiv:2505.14646", "arXiv:2509.15246"}, 30))
	if err != nil || res.Status != 200 {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if feed.Title != "Cites: Code generation" || len(feed.Items) != 2 {
		t.Fatalf("title %q, %d items", feed.Title, len(feed.Items))
	}
	// newest first, the seed itself (W2) excluded
	first, second := feed.Items[0], feed.Items[1]
	if first.Title != "A journal paper" || first.Link != "https://doi.org/10.1000/xyz" {
		t.Errorf("first = %+v", first)
	}
	if second.Title != "Newest arXiv paper" || second.Link != "https://arxiv.org/abs/2611.00001" || second.Description != "We cite CAD-Coder and extend it" {
		t.Errorf("second = %+v", second)
	}
	if second.PublishedParsed == nil || second.PublishedParsed.Format("2006-01-02") != "2026-11-10" {
		t.Errorf("published = %v", second.PublishedParsed)
	}

	// 2 seed lookups (the duplicate reference is looked up once) + the citation query
	if len(*requests) != 3 {
		t.Fatalf("requests = %d: %v", len(*requests), *requests)
	}
	last := (*requests)[2]
	if !strings.HasPrefix(last.Get("filter"), "cites:W1|W2,from_publication_date:") || last.Get("mailto") != "me@example.com" || last.Get("select") == "" {
		t.Errorf("citation query = %v", last)
	}
}

func TestFetchOpenAlexCitesWhenSeedsAreMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"results":[]}`)) }))
	defer srv.Close()
	useFakeOpenAlex(t, srv)
	_, _, err := fetchFeed(gofeed.NewParser(), citationFeedURL("x", []string{"arXiv:2612.99999"}, 30))
	if err == nil || !strings.Contains(err.Error(), "none of the papers to watch could be found") || !strings.Contains(err.Error(), "no record of arXiv:2612.99999") {
		t.Errorf("err = %v", err)
	}
}

func TestOpenAlexRetriesOn429(t *testing.T) {
	srv, _ := fakeOpenAlex(t)
	calls := 0
	retry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "3")
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		srv.Config.Handler.ServeHTTP(w, r)
	}))
	defer retry.Close()
	useFakeOpenAlex(t, retry)
	waits := noSleep(t)

	if _, err := resolveWork("arXiv:2509.15246"); err != nil || calls != 2 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
	if len(*waits) != 1 || (*waits)[0].Seconds() != 3 {
		t.Errorf("waits = %v", *waits)
	}
}

// A watch shows up as an ordinary section.
func TestProcessFeedWithCitationWatch(t *testing.T) {
	srv, _ := fakeOpenAlex(t)
	useFakeOpenAlex(t, srv)
	out := runFeed(t, RSS{url: citationFeedURL("Code generation", []string{"arXiv:2509.15246", "arXiv:2505.14646"}, 30), limit: 10, name: "Cites: Code generation"},
		&Config{ShowEmptySections: true}, testStore(t))
	for _, want := range []string{"HEADER[Cites: Code generation]", "LINK[A journal paper]", "LINK[Newest arXiv paper]"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestLoadCitationWatch(t *testing.T) {
	viper.Reset()
	defer viper.Reset()
	viper.SetConfigType("yaml")
	if err := viper.ReadConfig(strings.NewReader(`
citation_watch:
  - name: Core papers
    analyst: CAD research scout
    papers:
      - arXiv:2509.15246   # GenCAD-3D
      - arXiv:2505.14646
      - not a paper
  - name: Listed
    use: feed
    days: 7
    papers: [W123]
  - name: Nothing usable
    papers: ["???"]
  - name: Bad use
    use: sideways
    papers: [W1]
`)); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{}
	feeds, analyst := loadCitationWatch(cfg)
	if len(cfg.CitationWatch) != 4 {
		t.Fatalf("config watches = %d", len(cfg.CitationWatch))
	}
	if len(analyst) != 1 || analyst[0].analyst != "CAD research scout" || analyst[0].source() != "citation_watch" || !strings.HasSuffix(analyst[0].spec, " 50") {
		t.Fatalf("analyst = %+v", analyst)
	}
	u, _ := url.Parse(strings.Fields(analyst[0].spec)[0])
	if refs := u.Query()["ref"]; len(refs) != 2 || refs[0] != "arXiv:2509.15246" || u.Query().Get("days") != "30" {
		t.Errorf("analyst refs/days = %v / %s", refs, u.Query().Get("days"))
	}
	if len(feeds) != 1 || feeds[0].name != "Cites: Listed" || feeds[0].limit != 30 || feeds[0].summarize {
		t.Fatalf("feeds = %+v", feeds)
	}
	if u, _ := url.Parse(feeds[0].url); u.Query().Get("days") != "7" || u.Query()["ref"][0] != "W123" {
		t.Errorf("feed url = %s", feeds[0].url)
	}
}

func TestRunAnchors(t *testing.T) {
	srv, _ := fakeOpenAlex(t)
	useFakeOpenAlex(t, srv)
	cfg := &Config{CitationWatch: []CitationWatch{{Name: "x", Papers: []string{"arXiv:2509.15246", "arXiv:2505.14646"}}}}
	out := captureStdout(t, func() { runAnchors(cfg) })
	for _, want := range []string{"2 papers found in OpenAlex, 2 with a reference list", "2 of 2  DeepCAD (2021, cited 500 times)  [arXiv:2105.09492]", "- arXiv:2105.09492   # DeepCAD", "citation_watch:"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "W999") || strings.Contains(out, "W101") || strings.Contains(out, "W102") {
		t.Errorf("references cited by only one paper were listed:\n%s", out)
	}
}

func TestStoredPaperRefs(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"2610.13309v1.md", "cs_0501001v2.md", "notes.md", ".matcha-123", "2611.00002.zip"} {
		writeFileAtomic(filepath.Join(dir, name), []byte("x"))
	}
	got := storedPaperRefs(dir)
	want := map[string]bool{"arXiv:2610.13309": true, "arXiv:cs/0501001": true}
	if len(got) != 2 || !want[got[0]] || !want[got[1]] {
		t.Errorf("refs = %v", got)
	}
}

// The same paper arrives as http://arxiv.org/abs/<id>v1 (arXiv) and https://arxiv.org/abs/<id> (OpenAlex).
func TestIsSeenRecognizesOtherArxivSpellings(t *testing.T) {
	store := testStore(t)
	insert := func(u, date string) {
		if _, err := store.db.Exec("INSERT INTO seen(url, date, summary, title, feed_title) values(?,?,?,?,?)", u, date, "sum", "t", "f"); err != nil {
			t.Fatal(err)
		}
	}
	insert("http://arxiv.org/abs/2509.15246v1", "2000-01-01")
	insert("https://arxiv.org/abs/2601.12641#analyst:scout", "2000-01-01") // analyst bookkeeping only

	for _, link := range []string{"https://arxiv.org/abs/2509.15246", "http://arxiv.org/abs/2509.15246v2", "https://arxiv.org/pdf/2509.15246"} {
		if seen, _, summary := store.IsSeen(link); !seen || summary != "sum" {
			t.Errorf("IsSeen(%q) = %v %q", link, seen, summary)
		}
	}
	// the analyst's own row does not make the paper "listed", but does count for the analyst's key
	if seen, _, _ := store.IsSeen("https://arxiv.org/abs/2601.12641"); seen {
		t.Error("analyst bookkeeping row made a plain link seen")
	}
	if seen, _, _ := store.IsSeen("http://arxiv.org/abs/2601.12641v1#analyst:scout"); !seen {
		t.Error("analyst key not matched across spellings")
	}
	if seen, _, _ := store.IsSeen("http://arxiv.org/abs/2601.12641v1#analyst:other"); seen {
		t.Error("a different analyst's row matched")
	}
	if seen, _, _ := store.IsSeen("https://arxiv.org/abs/2509.1524"); seen { // a different, shorter ID
		t.Error("prefix of an ID matched")
	}
	if seen, _, _ := store.IsSeen("https://example.com/2509.15246"); seen {
		t.Error("non-arXiv link matched")
	}
}
