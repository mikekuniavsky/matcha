package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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

// MinerUClient converts PDFs to markdown through a running MinerU API server
// (`mineru-api`, POST /file_parse).
type MinerUClient struct {
	baseURL string
	http    *http.Client
}

func NewMinerUClient(baseURL string) *MinerUClient {
	if baseURL == "" {
		return nil
	}
	return &MinerUClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 10 * time.Minute}, // layout/OCR models can be slow on CPU
	}
}

// PDFToMarkdown downloads the PDF at pdfURL and returns MinerU's markdown.
func (m *MinerUClient) PDFToMarkdown(pdfURL string) (string, error) {
	dl := &http.Client{Timeout: 2 * time.Minute}
	resp, err := dl.Get(pdfURL)
	if err != nil {
		return "", fmt.Errorf("download pdf: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download pdf: status %s", resp.Status)
	}
	pdf, err := io.ReadAll(io.LimitReader(resp.Body, 50<<20))
	if err != nil {
		return "", fmt.Errorf("download pdf: %w", err)
	}
	return m.ParsePDF(pdf, pdfFileName(pdfURL))
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

// ParsePDF uploads pdf bytes to MinerU and returns the markdown of the first result.
func (m *MinerUClient) ParsePDF(pdf []byte, filename string) (string, error) {
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
