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
