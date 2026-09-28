package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestArxivPDFURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://arxiv.org/abs/2401.01234":        "https://arxiv.org/pdf/2401.01234",
		"https://arxiv.org/abs/2401.01234v2":      "https://arxiv.org/pdf/2401.01234v2",
		"http://www.arxiv.org/pdf/2401.01234.pdf": "https://arxiv.org/pdf/2401.01234",
	} {
		got, ok := arxivPDFURL(in)
		if !ok || got != want {
			t.Errorf("arxivPDFURL(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	if _, ok := arxivPDFURL("https://example.com/abs/1"); ok {
		t.Error("non-arxiv link matched")
	}
}

func TestMinerUParsePDF(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/file_parse" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		f, _, err := r.FormFile("files")
		if err != nil || r.FormValue("return_md") != "true" {
			http.Error(w, "bad request", 400)
			return
		}
		f.Close()
		w.Write([]byte(`{"results":{"paper":{"md_content":"# Title\n\nbody"}}}`))
	}))
	defer srv.Close()

	md, err := NewMinerUClient(srv.URL+"/").ParsePDF([]byte("%PDF-1.4"), "paper.pdf")
	if err != nil || md != "# Title\n\nbody" {
		t.Fatalf("got %q, %v", md, err)
	}
}
