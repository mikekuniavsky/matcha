package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func resultZip(t *testing.T, md string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{
		"markdown.md":             md,
		"middle_json.json":        `{"pdf_info":[]}`,
		"structured_content.json": `[]`,
		"images/fig1.png":         "PNG",
	} {
		w, _ := zw.Create(name)
		w.Write([]byte(body))
	}
	zw.Close()
	return buf.Bytes()
}

type fakeTasksOpts struct {
	urlOK     bool // tasks with a "url" input succeed
	failParse bool // the parse itself fails
}

type fakeTasksLog struct {
	urlTasks, uploads, cancels int
	lastReq                    map[string]interface{}
}

// fakeMinerUTasks follows MinerU 4.1's Tasks API (POST /v1/tasks, PUT upload, GET task, result ZIP).
func fakeMinerUTasks(t *testing.T, o fakeTasksOpts, lg *fakeTasksLog) *httptest.Server {
	zipBytes := resultZip(t, "# Parsed paper")
	status, fileStatus, polls := "", "", 0
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"healthy","version":"4.1.7","features":{"webhook":false,"task_output_formats":["default","docx","html","latex"],"sources":["upload","url"]}}`))
	})
	servePDF := func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("%PDF-1.4 fake")) }
	mux.HandleFunc("/pdf", servePDF)
	mux.HandleFunc("/paper.pdf", servePDF)
	// The 4.0 job API is gone in 4.1.
	mux.HandleFunc("/v1/parse/jobs", http.NotFound)
	mux.HandleFunc("/v1/uploads", http.NotFound)
	mux.HandleFunc("/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" || json.NewDecoder(r.Body).Decode(&req) != nil {
			http.Error(w, `{"error":{"code":"invalid_request","message":"json only"}}`, 400)
			return
		}
		lg.lastReq = req
		files, _ := req["files"].([]interface{})
		formats, _ := req["output_formats"].([]interface{})
		if len(files) != 1 || len(formats) != 1 || formats[0] != "default" || req["tier"] != "basic" {
			http.Error(w, `{"error":{"code":"invalid_request","message":"bad manifest"}}`, 400)
			return
		}
		f := files[0].(map[string]interface{})
		if _, bad := f["file_id"]; bad || f["file_name"] == "" {
			http.Error(w, `{"error":{"code":"invalid_request","message":"bad file"}}`, 400)
			return
		}
		polls = 0
		if _, ok := f["url"].(string); ok {
			lg.urlTasks++
			status, fileStatus = "pending", "pending"
			if !o.urlOK {
				status, fileStatus = "failed", "failed"
			}
		} else {
			if f["bytes"] != float64(len("%PDF-1.4 fake")) || len(f["sha256sum"].(string)) != 64 {
				http.Error(w, `{"error":{"code":"invalid_request","message":"bytes/sha"}}`, 400)
				return
			}
			status, fileStatus = "waiting-file", "waiting-file"
		}
		w.WriteHeader(202)
		w.Write([]byte(taskJSON(srv.URL, status, fileStatus, polls)))
	})
	mux.HandleFunc("/v1/tasks/task-1/uploads/file-1", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPut || string(b) != "%PDF-1.4 fake" || r.Header.Get("Content-Type") != "application/octet-stream" || fileStatus != "waiting-file" {
			http.Error(w, `{"error":{"code":"invalid_request","message":"bad upload"}}`, 400)
			return
		}
		lg.uploads++
		status, fileStatus = "pending", "pending"
		w.Write([]byte(`{"task_id":"task-1","file_id":"file-1","status":"pending"}`))
	})
	mux.HandleFunc("/v1/tasks/task-1", func(w http.ResponseWriter, r *http.Request) {
		if status == "pending" || status == "running" {
			polls++
			status, fileStatus = "running", "running"
			if polls >= 2 {
				status, fileStatus = "completed", "completed"
				if o.failParse {
					status, fileStatus = "failed", "failed"
				}
			}
		}
		w.Write([]byte(taskJSON(srv.URL, status, fileStatus, polls)))
	})
	mux.HandleFunc("/v1/tasks/task-1/cancel", func(w http.ResponseWriter, r *http.Request) {
		lg.cancels++
		w.Write([]byte(`{"task_id":"task-1","status":"canceled","files":[]}`))
	})
	mux.HandleFunc("/v1/files/file-1/content", func(w http.ResponseWriter, r *http.Request) {
		if status != "completed" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Write(zipBytes)
	})
	srv = httptest.NewServer(mux)
	return srv
}

func taskJSON(base, status, fileStatus string, polls int) string {
	f := map[string]interface{}{"file_id": "file-1", "file_name": "paper.pdf", "status": fileStatus}
	switch fileStatus {
	case "waiting-file":
		f["upload"] = map[string]interface{}{
			"url":        base + "/v1/tasks/task-1/uploads/file-1",
			"headers":    map[string]string{"Content-Type": "application/octet-stream"},
			"expires_at": 1,
		}
	case "completed":
		f["result_url"] = base + "/v1/files/file-1/content"
	case "failed":
		f["error"] = map[string]string{"code": "parse_failed", "message": "could not parse"}
	}
	b, _ := json.Marshal(map[string]interface{}{"task_id": "task-1", "status": status, "files": []interface{}{f}})
	return string(b)
}

func TestMinerUProbe(t *testing.T) {
	var lg fakeTasksLog
	tasks := fakeMinerUTasks(t, fakeTasksOpts{}, &lg)
	defer tasks.Close()
	var n int
	jobs := fakeMinerU(t, true, &n)
	defer jobs.Close()
	loading := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"code":"model_preload_pending","message":"loading"}}`, 503)
	}))
	defer loading.Close()
	old := httptest.NewServer(http.NotFoundHandler())
	defer old.Close()

	for srv, want := range map[*httptest.Server]string{tasks: mineruTasks, jobs: mineruJobs, old: mineruLegacy} {
		if got, err := testClient(srv.URL).probe(); err != nil || got != want {
			t.Errorf("probe(%s) = %q, %v; want %q", srv.URL, got, err, want)
		}
	}
	if _, err := testClient(loading.URL).probe(); err == nil || !strings.Contains(err.Error(), "not ready") {
		t.Errorf("a loading server should be reported, got %v", err)
	}
}

func TestVersionAtLeast(t *testing.T) {
	for v, want := range map[string]bool{"4.1.7": true, "4.1.0": true, "4.2": true, "5.0.0": true, "4.0.12": false, "3.4.5": false, "": false, "dev": false} {
		if got := versionAtLeast(v, 4, 1); got != want {
			t.Errorf("versionAtLeast(%q) = %v", v, got)
		}
	}
}

// arXiv-style URL without .pdf: no URL task, the PDF is uploaded with its size and hash.
func TestMinerUTasksUpload(t *testing.T) {
	var lg fakeTasksLog
	srv := fakeMinerUTasks(t, fakeTasksOpts{urlOK: true}, &lg)
	defer srv.Close()
	out, err := testClient(srv.URL).PDFToOutput(srv.URL+"/pdf", true)
	if err != nil || out.Markdown != "# Parsed paper" || lg.urlTasks != 0 || lg.uploads != 1 {
		t.Fatalf("got %q, %v, url tasks %d, uploads %d", out.Markdown, err, lg.urlTasks, lg.uploads)
	}
	if md, err := markdownFromResultZip(out.Zip); err != nil || md != "# Parsed paper" {
		t.Errorf("wantZip should keep the result zip, got %v", err)
	}
	if lg.lastReq["ocr_mode"] != "auto" {
		t.Errorf("request: %v", lg.lastReq)
	}
}

func TestMinerUTasksByURL(t *testing.T) {
	var lg fakeTasksLog
	srv := fakeMinerUTasks(t, fakeTasksOpts{urlOK: true}, &lg)
	defer srv.Close()
	out, err := testClient(srv.URL).PDFToOutput(srv.URL+"/paper.pdf", false)
	if err != nil || out.Markdown != "# Parsed paper" || out.Zip != nil || lg.urlTasks != 1 || lg.uploads != 0 {
		t.Fatalf("got %q, %v, zip %d bytes, url tasks %d, uploads %d", out.Markdown, err, len(out.Zip), lg.urlTasks, lg.uploads)
	}
}

func TestMinerUTasksURLFailsThenUpload(t *testing.T) {
	var lg fakeTasksLog
	srv := fakeMinerUTasks(t, fakeTasksOpts{}, &lg)
	defer srv.Close()
	md, err := testClient(srv.URL).PDFToMarkdown(srv.URL + "/paper.pdf")
	if err != nil || md != "# Parsed paper" || lg.urlTasks != 1 || lg.uploads != 1 {
		t.Fatalf("got %q, %v, url tasks %d, uploads %d", md, err, lg.urlTasks, lg.uploads)
	}
}

func TestMinerUTasksParseFailure(t *testing.T) {
	var lg fakeTasksLog
	srv := fakeMinerUTasks(t, fakeTasksOpts{failParse: true}, &lg)
	defer srv.Close()
	_, err := testClient(srv.URL).PDFToMarkdown(srv.URL + "/pdf")
	if err == nil || !strings.Contains(err.Error(), "could not parse") {
		t.Fatalf("want the server's error message, got %v", err)
	}
}

// A task that never gets going (waiting-file without an upload link) times out and is canceled.
func TestMinerUTasksTimeoutCancels(t *testing.T) {
	cancels := 0
	stuck := `{"task_id":"task-x","status":"waiting-file","files":[{"file_id":"f","file_name":"p.pdf","status":"waiting-file"}]}`
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/tasks", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(202); w.Write([]byte(stuck)) })
	mux.HandleFunc("/v1/tasks/task-x", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(stuck)) })
	mux.HandleFunc("/v1/tasks/task-x/cancel", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			cancels++
		}
		w.Write([]byte(`{"task_id":"task-x","status":"canceled","files":[]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := testClient(srv.URL)
	c.jobTimeout = 0
	_, err := c.runTask(map[string]interface{}{"file_name": "p.pdf", "bytes": 1}, []byte("x"), false)
	if err == nil || !strings.Contains(err.Error(), "timed out") || cancels != 1 {
		t.Fatalf("got %v, cancels %d", err, cancels)
	}
}

func TestMarkdownFromResultZip(t *testing.T) {
	if _, err := markdownFromResultZip([]byte("not a zip")); err == nil {
		t.Error("non-zip accepted")
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("images/a.png")
	w.Write([]byte("x"))
	zw.Close()
	if _, err := markdownFromResultZip(buf.Bytes()); err == nil {
		t.Error("zip without markdown accepted")
	}
}
