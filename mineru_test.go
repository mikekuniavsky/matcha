package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

func TestMinerULegacyParse(t *testing.T) {
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

	md, err := NewMinerUClient(srv.URL+"/", "").legacyParse([]byte("%PDF-1.4"), "paper.pdf")
	if err != nil || md != "# Title\n\nbody" {
		t.Fatalf("got %q, %v", md, err)
	}
}

// fakeMinerU follows the /v1 schema of MinerU 3.x (uploads, parse jobs, files).
// urlOK controls whether jobs may use a "url" source.
func fakeMinerU(t *testing.T, urlOK bool) *httptest.Server {
	polls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"status":"ok","version":"3.4.5"}`)) })
	mux.HandleFunc("/pdf", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("%PDF-1.4 fake")) })
	mux.HandleFunc("/v1/uploads", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"upload_1","bytes":13,"status":"pending"}`))
	})
	mux.HandleFunc("/v1/uploads/upload_1/content", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPut || string(b) != "%PDF-1.4 fake" {
			http.Error(w, "bad upload", 400)
			return
		}
		w.Write([]byte(`{}`))
	})
	mux.HandleFunc("/v1/uploads/upload_1/complete", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"upload_1","status":"completed","file":{"id":"file-src"}}`))
	})
	mux.HandleFunc("/v1/parse/jobs", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Files []struct {
				Source map[string]string `json:"source"`
			} `json:"files"`
			Formats []string `json:"output_formats"`
			Tier    string   `json:"tier"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		src := req.Files[0].Source
		if src["type"] == "url" && !urlOK {
			w.WriteHeader(400)
			w.Write([]byte(`{"error":{"type":"invalid_request","message":"url sources disabled"}}`))
			return
		}
		if src["type"] == "file_id" && src["file_id"] != "file-src" || len(req.Formats) != 1 || req.Formats[0] != "markdown" || req.Tier != "basic" {
			http.Error(w, "bad job", 400)
			return
		}
		w.WriteHeader(202)
		w.Write([]byte(`{"job_id":"job_1","status":"queued","files":[]}`))
	})
	mux.HandleFunc("/v1/parse/jobs/job_1", func(w http.ResponseWriter, r *http.Request) {
		polls++
		if polls < 2 {
			w.Write([]byte(`{"job_id":"job_1","status":"running","files":[]}`))
			return
		}
		w.Write([]byte(`{"job_id":"job_1","status":"completed","files":[{"name":"p.pdf","status":"completed","output_files":{"markdown":{"file_id":"file-md","bytes":9}}}]}`))
	})
	mux.HandleFunc("/v1/files/file-md/content", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("# Parsed paper")) })
	mux.HandleFunc("/v1/files/", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"deleted":true}`)) })
	return httptest.NewServer(mux)
}

func testClient(url string) *MinerUClient {
	c := NewMinerUClient(url, "basic")
	c.pollInterval, c.retryDelay = time.Millisecond, time.Millisecond
	return c
}

func TestMinerUV1ByURL(t *testing.T) {
	srv := fakeMinerU(t, true)
	defer srv.Close()
	md, err := testClient(srv.URL).PDFToMarkdown(srv.URL + "/pdf")
	if err != nil || md != "# Parsed paper" {
		t.Fatalf("got %q, %v", md, err)
	}
}

func TestMinerUV1UploadFallback(t *testing.T) {
	srv := fakeMinerU(t, false)
	defer srv.Close()
	md, err := testClient(srv.URL).PDFToMarkdown(srv.URL + "/pdf")
	if err != nil || md != "# Parsed paper" {
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
