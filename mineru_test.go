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

func TestSplitSelection(t *testing.T) {
	items := []analystItem{{Title: "a"}, {Title: "b"}, {Title: "c"}, {Title: "d"}}
	out, got := splitSelection("Pick b and d.\nSELECTED: 2, 4, 2, 9, 3", items, 3)
	if out != "Pick b and d." {
		t.Errorf("analysis = %q", out)
	}
	if len(got) != 3 || got[0].Title != "b" || got[1].Title != "d" || got[2].Title != "c" {
		t.Errorf("picked = %+v", got)
	}
	if _, got := splitSelection("nothing\nSELECTED: none", items, 3); len(got) != 0 {
		t.Errorf("none picked %+v", got)
	}
	if out, got := splitSelection("no marker", items, 3); out != "no marker" || got != nil {
		t.Errorf("no marker: %q %v", out, got)
	}
}

func TestLinkCitations(t *testing.T) {
	items := []analystItem{{Link: "https://a"}, {Link: "https://b"}}
	got := linkCitations("See [2] and [1], not [7].", items)
	want := "See [[2]](https://b) and [[1]](https://a), not [7]."
	if got != want {
		t.Errorf("got %q", got)
	}
}

func TestUnwrapGoogleRedirect(t *testing.T) {
	in := "https://www.google.com/url?rct=j&sa=t&url=https://example.com/post%3Fid%3D1&ct=ga&cd=abc"
	if got := unwrapGoogleRedirect(in); got != "https://example.com/post?id=1" {
		t.Errorf("got %q", got)
	}
	if got := unwrapGoogleRedirect("https://arxiv.org/abs/1"); got != "https://arxiv.org/abs/1" {
		t.Errorf("changed non-google link: %q", got)
	}
}
