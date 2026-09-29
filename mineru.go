package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var arxivIDRe = regexp.MustCompile(`^https?://(?:www\.)?arxiv\.org/(?:abs|pdf)/([^?#]+?)(?:\.pdf)?$`)

// arxivPDFURL returns the PDF URL for an arXiv abstract/pdf link.
func arxivPDFURL(link string) (string, bool) {
	m := arxivIDRe.FindStringSubmatch(link)
	if m == nil {
		return "", false
	}
	return "https://arxiv.org/pdf/" + m[1], true
}

// MinerUClient converts PDFs to markdown through a running MinerU API server.
//
// Current servers expose an asynchronous job API (/v1/parse/jobs): create a job for a
// URL (or uploaded file), poll it, then download the markdown output. Older
// `mineru-api` servers expose a synchronous POST /file_parse; that is used when
// /v1/health is not available.
type MinerUClient struct {
	baseURL      string
	tier         string // optional: flash, basic, standard or advanced
	http         *http.Client
	pollInterval time.Duration
	jobTimeout   time.Duration
	retryDelay   time.Duration
}

func NewMinerUClient(baseURL, tier string) *MinerUClient {
	if baseURL == "" {
		return nil
	}
	return &MinerUClient{
		baseURL:      strings.TrimRight(baseURL, "/"),
		tier:         tier,
		http:         &http.Client{Timeout: 10 * time.Minute}, // layout/OCR models can be slow on CPU
		pollInterval: 3 * time.Second,
		jobTimeout:   15 * time.Minute,
		retryDelay:   10 * time.Second,
	}
}

// PDFToMarkdown returns MinerU's markdown for the PDF at pdfURL.
func (m *MinerUClient) PDFToMarkdown(pdfURL string) (string, error) {
	if !m.hasV1() {
		pdf, err := downloadPDF(pdfURL)
		if err != nil {
			return "", err
		}
		return m.legacyParse(pdf, pdfFileName(pdfURL))
	}

	// Let the server fetch the PDF itself; fall back to uploading it if that fails.
	md, err := m.v1Parse(map[string]string{"type": "url", "url": pdfURL})
	if err == nil {
		return md, nil
	}
	log.Printf("MinerU could not parse %s by URL (%v); uploading the PDF instead", pdfURL, err)
	pdf, err := downloadPDF(pdfURL)
	if err != nil {
		return "", err
	}
	fileID, err := m.upload(pdf, pdfFileName(pdfURL))
	if err != nil {
		return "", err
	}
	defer m.callJSON(http.MethodDelete, "/v1/files/"+fileID, nil, nil) // best-effort cleanup
	return m.v1Parse(map[string]string{"type": "file_id", "file_id": fileID})
}

func downloadPDF(pdfURL string) ([]byte, error) {
	dl := &http.Client{Timeout: 2 * time.Minute}
	resp, err := dl.Get(pdfURL)
	if err != nil {
		return nil, fmt.Errorf("download pdf: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download pdf: status %s", resp.Status)
	}
	pdf, err := io.ReadAll(io.LimitReader(resp.Body, 50<<20))
	if err != nil {
		return nil, fmt.Errorf("download pdf: %w", err)
	}
	return pdf, nil
}

func pdfFileName(pdfURL string) string {
	u, err := url.Parse(pdfURL)
	if err != nil {
		return "paper.pdf"
	}
	name := strings.ReplaceAll(strings.Trim(u.Path, "/"), "/", "_")
	name = strings.TrimSuffix(name, ".pdf")
	if name == "" {
		name = "paper"
	}
	return name + ".pdf"
}

// ---- v1 job API ----

type v1Job struct {
	JobID  string `json:"job_id"`
	Status string `json:"status"`
	Files  []struct {
		Name        string `json:"name"`
		Status      string `json:"status"`
		OutputFiles *struct {
			Markdown *struct {
				FileID string `json:"file_id"`
			} `json:"markdown"`
		} `json:"output_files"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	} `json:"files"`
}

func (j v1Job) errorText() string {
	for _, f := range j.Files {
		if f.Error != nil && f.Error.Message != "" {
			return ": " + f.Error.Message
		}
	}
	return ""
}

func (m *MinerUClient) hasV1() bool {
	resp, err := m.http.Get(m.baseURL + "/v1/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode == http.StatusOK
}

// callJSON sends an optional JSON body and decodes an optional JSON reply.
// The HTTP status is returned even when err != nil.
func (m *MinerUClient) callJSON(method, path string, in, out interface{}) (int, error) {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return 0, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, m.baseURL+path, body)
	if err != nil {
		return 0, err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := m.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("mineru %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		msg := fmt.Sprintf("%.200s", raw)
		if json.Unmarshal(raw, &e) == nil && e.Error.Message != "" {
			msg = e.Error.Message
		}
		return resp.StatusCode, fmt.Errorf("mineru %s %s: %s: %s", method, path, resp.Status, msg)
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return resp.StatusCode, fmt.Errorf("mineru %s %s: bad json: %w", method, path, err)
		}
	}
	return resp.StatusCode, nil
}

// v1Parse creates a markdown parse job for one source, waits for it, and returns the markdown.
func (m *MinerUClient) v1Parse(source map[string]string) (string, error) {
	req := map[string]interface{}{
		"files":          []interface{}{map[string]interface{}{"source": source}},
		"ocr_mode":       "auto",
		"output_formats": []string{"markdown"},
	}
	if m.tier != "" {
		req["tier"] = m.tier
	}

	var job v1Job
	for attempt := 0; ; attempt++ {
		status, err := m.callJSON(http.MethodPost, "/v1/parse/jobs", req, &job)
		if err == nil {
			break
		}
		if status == http.StatusTooManyRequests && attempt < 5 { // e.g. another job still running
			time.Sleep(m.retryDelay)
			continue
		}
		return "", err
	}

	jobID := job.JobID
	deadline := time.Now().Add(m.jobTimeout)
	for {
		switch job.Status {
		case "completed", "partial":
			return m.fetchMarkdown(job)
		case "failed", "canceled":
			return "", fmt.Errorf("mineru job %s %s%s", jobID, job.Status, job.errorText())
		}
		if time.Now().After(deadline) {
			m.callJSON(http.MethodDelete, "/v1/parse/jobs/"+jobID, nil, nil)
			return "", fmt.Errorf("mineru job %s timed out after %s", jobID, m.jobTimeout)
		}
		time.Sleep(m.pollInterval)
		job = v1Job{}
		if _, err := m.callJSON(http.MethodGet, "/v1/parse/jobs/"+jobID, nil, &job); err != nil {
			return "", err
		}
	}
}

func (m *MinerUClient) fetchMarkdown(job v1Job) (string, error) {
	for _, f := range job.Files {
		if f.OutputFiles == nil || f.OutputFiles.Markdown == nil {
			continue
		}
		id := f.OutputFiles.Markdown.FileID
		resp, err := m.http.Get(m.baseURL + "/v1/files/" + id + "/content")
		if err != nil {
			return "", fmt.Errorf("mineru download output: %w", err)
		}
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("mineru download output: status %s", resp.Status)
		}
		m.callJSON(http.MethodDelete, "/v1/files/"+id, nil, nil) // best-effort cleanup
		if md := string(raw); strings.TrimSpace(md) != "" {
			return md, nil
		}
	}
	return "", fmt.Errorf("mineru job %s finished without markdown output%s", job.JobID, job.errorText())
}

// upload sends pdf bytes to MinerU (create upload, PUT content, complete) and returns the file ID.
func (m *MinerUClient) upload(pdf []byte, filename string) (string, error) {
	type uploadResp struct {
		ID            string            `json:"id"`
		UploadURL     string            `json:"upload_url"`
		UploadHeaders map[string]string `json:"upload_headers"`
		File          *struct {
			ID string `json:"id"`
		} `json:"file"`
	}
	var up uploadResp
	if _, err := m.callJSON(http.MethodPost, "/v1/uploads", map[string]interface{}{
		"filename":  filename,
		"bytes":     len(pdf),
		"mime_type": "application/pdf",
		"purpose":   "parse",
	}, &up); err != nil {
		return "", err
	}

	putURL := m.baseURL + "/v1/uploads/" + up.ID + "/content"
	if up.UploadURL != "" {
		putURL = up.UploadURL
		if strings.HasPrefix(putURL, "/") {
			putURL = m.baseURL + putURL
		}
	}
	req, err := http.NewRequest(http.MethodPut, putURL, bytes.NewReader(pdf))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	for k, v := range up.UploadHeaders {
		req.Header.Set(k, v)
	}
	resp, err := m.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("mineru upload: %w", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("mineru upload: status %s", resp.Status)
	}

	var done uploadResp
	if _, err := m.callJSON(http.MethodPost, "/v1/uploads/"+up.ID+"/complete", map[string]interface{}{}, &done); err != nil {
		return "", err
	}
	if done.File == nil || done.File.ID == "" {
		return "", fmt.Errorf("mineru upload %s completed without a file id", up.ID)
	}
	return done.File.ID, nil
}

// ---- legacy synchronous API (older mineru-api: POST /file_parse) ----

// legacyParse uploads pdf bytes to MinerU and returns the markdown of the first result.
func (m *MinerUClient) legacyParse(pdf []byte, filename string) (string, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("files", filename)
	if err != nil {
		return "", err
	}
	if _, err := fw.Write(pdf); err != nil {
		return "", err
	}
	for k, v := range map[string]string{
		"return_md":          "true",
		"return_middle_json": "false",
		"return_images":      "false",
		"formula_enable":     "true",
		"table_enable":       "true",
	} {
		_ = mw.WriteField(k, v)
	}
	if err := mw.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequest(http.MethodPost, m.baseURL+"/file_parse", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := m.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("mineru request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("mineru response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("mineru: status %s: %.200s", resp.Status, raw)
	}

	var parsed struct {
		Results map[string]struct {
			MD string `json:"md_content"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("mineru: bad json: %w", err)
	}
	for _, r := range parsed.Results {
		if r.MD != "" {
			return r.MD, nil
		}
	}
	return "", fmt.Errorf("mineru: no markdown in response")
}
