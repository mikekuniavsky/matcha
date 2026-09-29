package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchArticleText(t *testing.T) {
	para := strings.Repeat("Backflip AI converts engineering drawings into editable CAD models. ", 12)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.UserAgent(), "Go-http-client") {
			http.Error(w, "bot", http.StatusForbidden)
			return
		}
		w.Write([]byte("<html><head><title>T</title></head><body><article><h1>Title</h1><p>" + para + "</p><p>" + para + "</p></article></body></html>"))
	}))
	defer srv.Close()

	text, err := fetchArticleText(srv.URL)
	if err != nil || !strings.Contains(text, "editable CAD models") {
		t.Fatalf("got %q, %v", text, err)
	}
	if _, err := fetchArticleText(srv.URL + "/x"); err != nil {
		t.Fatal(err) // fake server serves any path
	}
}
