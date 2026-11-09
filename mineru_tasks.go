package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

// MinerU 4.1+ Tasks API (/v1/tasks). It replaced the /v1/uploads + /v1/parse/jobs flow:
//
//	POST /v1/tasks                  JSON manifest {files:[{file_name, bytes|url, sha256sum}], ...}
//	PUT  files[].upload.url         raw PDF bytes, only while the file is "waiting-file"
//	GET  /v1/tasks/{task_id}        poll until the task is completed/partial/failed/canceled
//	GET  files[].result_url         a ZIP with markdown.md, middle_json.json,
//	                                structured_content.json and images/
//	POST /v1/tasks/{task_id}/cancel on timeout
//
// There is no standalone markdown output any more and no DELETE for results: the server
// expires them itself.

type taskError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *taskError) String() string {
	if e == nil || e.Message == "" && e.Code == "" {
		return ""
	}
	if e.Code == "" {
		return ": " + e.Message
	}
	return ": " + e.Code + ": " + e.Message
}

type taskFile struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	Status   string `json:"status"`
	Upload   *struct {
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
	} `json:"upload"`
	ResultURL string     `json:"result_url"`
	Error     *taskError `json:"error"`
}

type taskResp struct {
	TaskID string     `json:"task_id"`
	Status string     `json:"status"`
	Files  []taskFile `json:"files"`
	Error  *taskError `json:"error"`
}

func (t taskResp) file() taskFile {
	if len(t.Files) == 0 {
		return taskFile{}
	}
	return t.Files[0]
}

func (t taskResp) terminal() bool {
	switch t.Status {
	case "completed", "partial", "failed", "canceled":
		return true
	}
	return false
}

// tasksOutput parses pdfURL with the Tasks API: by URL when the server can fetch it (the
// URL ends in .pdf), otherwise, or if that fails, by downloading the PDF and uploading it.
func (m *MinerUClient) tasksOutput(pdfURL string, wantZip bool) (MinerUOutput, error) {
	name := pdfFileName(pdfURL)
	if hasPDFExtension(pdfURL) {
		out, err := m.runTask(map[string]interface{}{"file_name": name, "url": pdfURL}, nil, wantZip)
		if err == nil {
			return out, nil
		}
		log.Printf("MinerU could not parse %s by URL (%v); uploading the PDF instead", pdfURL, err)
	}
	pdf, err := downloadPDF(pdfURL)
	if err != nil {
		return MinerUOutput{}, err
	}
	sum := sha256.Sum256(pdf)
	return m.runTask(map[string]interface{}{
		"file_name": name,
		"bytes":     len(pdf),
		"sha256sum": hex.EncodeToString(sum[:]),
	}, pdf, wantZip)
}

// runTask creates a one-file task, uploads pdf when the server asks for it, waits for the
// task and returns the markdown (and the result ZIP when wantZip).
func (m *MinerUClient) runTask(file map[string]interface{}, pdf []byte, wantZip bool) (MinerUOutput, error) {
	req := map[string]interface{}{
		"files":          []interface{}{file},
		"ocr_mode":       "auto",
		"output_formats": []string{"default"},
	}
	if m.tier != "" {
		req["tier"] = m.tier
	}

	var task taskResp
	for attempt := 0; ; attempt++ {
		status, err := m.callJSON(http.MethodPost, "/v1/tasks", req, &task)
		if err == nil {
			break
		}
		if status == http.StatusTooManyRequests && attempt < 5 {
			time.Sleep(m.retryDelay)
			continue
		}
		return MinerUOutput{}, err
	}
	if task.TaskID == "" || len(task.Files) != 1 {
		return MinerUOutput{}, fmt.Errorf("mineru POST /v1/tasks: unexpected reply (task %q, %d files)", task.TaskID, len(task.Files))
	}
	taskPath := "/v1/tasks/" + url.PathEscape(task.TaskID)

	uploaded := pdf == nil
	deadline := time.Now().Add(m.jobTimeout)
	for !task.terminal() {
		f := task.file()
		if !uploaded && f.Status == "waiting-file" && f.Upload != nil && f.Upload.URL != "" {
			if err := m.putTaskFile(f.Upload.URL, f.Upload.Headers, pdf); err != nil {
				m.callJSON(http.MethodPost, taskPath+"/cancel", nil, nil)
				return MinerUOutput{}, err
			}
			uploaded = true
		} else {
			if time.Now().After(deadline) {
				m.callJSON(http.MethodPost, taskPath+"/cancel", nil, nil)
				return MinerUOutput{}, fmt.Errorf("mineru task %s timed out after %s", task.TaskID, m.jobTimeout)
			}
			time.Sleep(m.pollInterval)
		}
		task = taskResp{}
		if _, err := m.callJSON(http.MethodGet, taskPath, nil, &task); err != nil {
			return MinerUOutput{}, err
		}
	}

	f := task.file()
	if f.Status != "completed" || f.ResultURL == "" {
		msg := f.Error.String()
		if msg == "" {
			msg = task.Error.String()
		}
		return MinerUOutput{}, fmt.Errorf("mineru task %s %s (file %s)%s", task.TaskID, task.Status, f.Status, msg)
	}
	raw, err := m.downloadResult(f.ResultURL)
	if err != nil {
		return MinerUOutput{}, err
	}
	md, err := markdownFromResultZip(raw)
	if err != nil {
		return MinerUOutput{}, fmt.Errorf("mineru task %s: %w", task.TaskID, err)
	}
	out := MinerUOutput{Markdown: md}
	if wantZip {
		out.Zip = raw
	}
	return out, nil
}

// resolve turns a server-given URL (absolute or relative) into an absolute one.
func (m *MinerUClient) resolve(ref string) (string, error) {
	base, err := url.Parse(m.baseURL + "/")
	if err != nil {
		return "", err
	}
	u, err := base.Parse(ref)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// putTaskFile uploads the PDF bytes to a task's upload URL with the headers the server gave.
func (m *MinerUClient) putTaskFile(ref string, headers map[string]string, pdf []byte) error {
	u, err := m.resolve(ref)
	if err != nil {
		return fmt.Errorf("mineru upload: %w", err)
	}
	req, err := http.NewRequest(http.MethodPut, u, bytes.NewReader(pdf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := m.http.Do(req)
	if err != nil {
		return fmt.Errorf("mineru upload: %w", err)
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("mineru upload: status %s: %.200s", resp.Status, raw)
	}
	return nil
}

// downloadResult fetches a completed file's result ZIP.
func (m *MinerUClient) downloadResult(ref string) ([]byte, error) {
	u, err := m.resolve(ref)
	if err != nil {
		return nil, fmt.Errorf("mineru download result: %w", err)
	}
	resp, err := m.http.Get(u)
	if err != nil {
		return nil, fmt.Errorf("mineru download result: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("mineru download result: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mineru download result: status %s: %.200s", resp.Status, raw)
	}
	return raw, nil
}

// markdownFromResultZip returns markdown.md from a Tasks result ZIP (or, failing that, the
// only top-level .md file).
func markdownFromResultZip(raw []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return "", fmt.Errorf("result is not a zip: %w", err)
	}
	var candidates []*zip.File
	for _, f := range zr.File {
		if f.Name == "markdown.md" {
			candidates = []*zip.File{f}
			break
		}
		if !strings.Contains(f.Name, "/") && path.Ext(f.Name) == ".md" {
			candidates = append(candidates, f)
		}
	}
	if len(candidates) != 1 {
		return "", fmt.Errorf("result zip has no markdown.md")
	}
	rc, err := candidates[0].Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	md, err := io.ReadAll(rc)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(string(md)) == "" {
		return "", fmt.Errorf("result markdown is empty")
	}
	return string(md), nil
}
