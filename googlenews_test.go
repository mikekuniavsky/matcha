package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestGoogleNewsID(t *testing.T) {
	id, ok := googleNewsID("https://news.google.com/rss/articles/CBMiabc?oc=5")
	if !ok || id != "CBMiabc" {
		t.Errorf("got %q %v", id, ok)
	}
	if _, ok := googleNewsID("https://example.com/rss/articles/x"); ok {
		t.Error("matched non-google host")
	}
}

// Exercises the decode flow against a fake server that follows the response shape
// the code expects; it can't prove Google's real endpoint still behaves this way.
func TestDecodeGoogleNewsURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/rss/articles/"):
			w.Write([]byte(`<c-wiz><div jscontroller="x" data-n-a-sg="SIG123" data-n-a-ts="1700000000"></div></c-wiz>`))
		case r.URL.Path == "/_/DotsSplashUi/data/batchexecute":
			r.ParseForm()
			f := r.PostForm.Get("f.req")
			if !strings.Contains(f, "Fbv4je") || !strings.Contains(f, "SIG123") || !strings.Contains(f, "1700000000") || !strings.Contains(f, "CBMiabc") {
				http.Error(w, "bad f.req: "+url.QueryEscape(f), 400)
				return
			}
			w.Write([]byte(")]}'\n\n[[\"wrb.fr\",\"Fbv4je\",\"[\\\"garturlres\\\",\\\"https://publisher.example/story\\\",1]\",null,null,null,\"generic\"],[\"di\",5]]\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	oldBase, oldDelay := googleNewsBase, googleNewsDelay
	googleNewsBase, googleNewsDelay = srv.URL, time.Millisecond
	defer func() { googleNewsBase, googleNewsDelay = oldBase, oldDelay }()

	got, err := decodeGoogleNewsURL("https://news.google.com/rss/articles/CBMiabc?oc=5")
	if err != nil || got != "https://publisher.example/story" {
		t.Fatalf("got %q, %v", got, err)
	}
}
