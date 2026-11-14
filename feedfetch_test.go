package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mmcdole/gofeed"
)

func noSleep(t *testing.T) *[]time.Duration {
	t.Helper()
	var waits []time.Duration
	old := sleepFor
	sleepFor = func(d time.Duration) { waits = append(waits, d) }
	t.Cleanup(func() { sleepFor = old })
	return &waits
}

// A host that answers 429 twice and then serves the feed: Matcha waits and retries.
func TestFetchFeedRetriesOn429(t *testing.T) {
	waits := noSleep(t)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.UserAgent(), "Matcha/") {
			t.Errorf("User-Agent = %q", r.UserAgent())
		}
		switch atomic.AddInt32(&calls, 1) {
		case 1:
			w.Header().Set("Retry-After", "7")
			http.Error(w, "slow down", http.StatusTooManyRequests)
		case 2:
			http.Error(w, "slow down", http.StatusTooManyRequests) // no Retry-After: default backoff
		default:
			w.Write([]byte(emptyRSS))
		}
	}))
	defer srv.Close()

	feed, res, err := fetchFeed(gofeed.NewParser(), srv.URL)
	if err != nil || feed.Title != "Empty Feed" || res.Attempts != 3 || res.Status != 200 {
		t.Fatalf("feed=%v res=%+v err=%v", feed, res, err)
	}
	if len(*waits) != 2 || (*waits)[0] != 7*time.Second || (*waits)[1] != retryDelays[1] {
		t.Errorf("waits = %v", *waits)
	}
}

func TestFetchFeedGivesUpAfterRetries(t *testing.T) {
	noSleep(t)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "no", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	_, res, err := fetchFeed(gofeed.NewParser(), srv.URL)
	if err == nil || !strings.Contains(err.Error(), "429 Too Many Requests") {
		t.Fatalf("err = %v", err)
	}
	if want := int32(len(retryDelays) + 1); calls != want || res.Attempts != int(want) || res.Status != 429 {
		t.Errorf("calls=%d res=%+v, want %d attempts", calls, res, want)
	}
}

func TestFetchFeedDoesNotRetryOtherErrors(t *testing.T) {
	waits := noSleep(t)
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	if _, res, err := fetchFeed(gofeed.NewParser(), srv.URL); err == nil || res.Attempts != 1 || len(*waits) != 0 {
		t.Errorf("err=%v res=%+v waits=%v", err, res, *waits)
	}
}

func TestRetryAfterParsing(t *testing.T) {
	if d, ok := retryAfter("12"); !ok || d != 12*time.Second {
		t.Errorf("seconds: %v %v", d, ok)
	}
	if d, ok := retryAfter("100000"); !ok || d != maxRetryAfter {
		t.Errorf("cap: %v %v", d, ok)
	}
	if _, ok := retryAfter(""); ok {
		t.Error("empty header accepted")
	}
	if d, ok := retryAfter(time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)); !ok || d != 0 {
		t.Errorf("past date: %v %v", d, ok)
	}
}

// Requests to a rate-limited host are spaced out; other hosts are not delayed.
func TestPoliteWaitSpacesRequests(t *testing.T) {
	waits := noSleep(t)
	minHostInterval["limited.test"] = 3 * time.Second
	defer delete(minHostInterval, "limited.test")
	hostLastRequest = map[string]time.Time{}

	politeWait("limited.test")
	politeWait("limited.test")
	politeWait("other.test")
	politeWait("other.test")
	if len(*waits) != 1 || (*waits)[0] <= 2*time.Second || (*waits)[0] > 3*time.Second {
		t.Errorf("waits = %v", *waits)
	}
}

func TestRunFeedCheckReportsEachFeed(t *testing.T) {
	good := feedServer(t, emptyRSS)
	bad := httptest.NewServer(http.NotFoundHandler())
	defer bad.Close()
	cfg := &Config{Feeds: []RSS{{url: good.URL, name: "Good feed"}, {url: bad.URL}}, AnalystFeeds: []string{good.URL + " 30"}}

	out := captureStdout(t, func() { runFeedCheck(cfg) })
	for _, want := range []string{"ok    200", "Good feed", "(0 items)", "FAIL  404", "analyst: "} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	f()
	w.Close()
	os.Stdout = old
	return <-done
}

func resetForbidden(t *testing.T) {
	t.Helper()
	old := forbiddenHosts
	forbiddenHosts = map[string]int{}
	t.Cleanup(func() { forbiddenHosts = old })
}

// A host that answers 403 twice in a row is skipped for the rest of the run, with no further
// requests (and no 429 retry waits) to it.
func TestFetchFeedGivesUpOnAHostThatKeepsSaying403(t *testing.T) {
	resetForbidden(t)
	waits := noSleep(t)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "no", http.StatusForbidden)
	}))
	defer srv.Close()

	for i := 0; i < 2; i++ {
		if _, res, err := fetchFeed(gofeed.NewParser(), srv.URL); err == nil || res.Status != 403 || !strings.Contains(err.Error(), "403 Forbidden") {
			t.Fatalf("attempt %d: res=%+v err=%v", i, res, err)
		}
	}
	_, res, err := fetchFeed(gofeed.NewParser(), srv.URL+"/another-feed")
	if err == nil || !strings.Contains(err.Error(), "skipped") || res.Attempts != 0 {
		t.Errorf("third fetch: res=%+v err=%v", res, err)
	}
	if calls != 2 || len(*waits) != 0 {
		t.Errorf("calls=%d waits=%v, want 2 requests and no waits", calls, *waits)
	}
}

// One 403 does not block a host, and a success resets the count.
func TestFetchFeedForbiddenCountResetsOnSuccess(t *testing.T) {
	resetForbidden(t)
	noSleep(t)
	status := []int{403, 200, 403, 200, 403}
	var i int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := status[int(atomic.AddInt32(&i, 1))-1]
		if s == 403 {
			http.Error(w, "no", s)
			return
		}
		w.Write([]byte(emptyRSS))
	}))
	defer srv.Close()
	for n := range status {
		_, _, err := fetchFeed(gofeed.NewParser(), srv.URL)
		if (status[n] == 403) != (err != nil) || (err != nil && strings.Contains(err.Error(), "skipped")) {
			t.Fatalf("fetch %d (status %d): err=%v", n, status[n], err)
		}
	}
}

// -check-feeds says which config file it read and what it loaded, so a block missing from that
// file (or indented under the wrong key) is visible at once.
func TestRunFeedCheckReportsWhatItLoaded(t *testing.T) {
	cfg := &Config{ConfigFile: "/home/me/matcha/config.yaml",
		CitationWatch: []CitationWatch{{Name: "a", Papers: []string{"arXiv:2509.15246", "arXiv:2505.14646"}}, {Name: "b", Papers: []string{"W1"}}},
		Analysts:      []AnalystConfig{{Name: "scout"}}}
	out := captureStdout(t, func() { runFeedCheck(cfg) })
	for _, want := range []string{"Config: /home/me/matcha/config.yaml", "Loaded: 0 feeds, 1 analysts, 2 citation watches (3 papers watched)"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}
