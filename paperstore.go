package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Parsed papers can be kept on disk so they are never parsed twice and can be used by other
// tools (Msty knowledge stacks, Obsidian, Zotero, grep). Files are named after the arXiv ID:
//
//	<dir>/2610.13309v1.md    front matter (title, source, date) + MinerU's markdown
//	<dir>/2610.13309v1.zip   MinerU's package (only with paper_store_zip: true)

// paperStoreID returns the file stem for an arXiv link ("2610.13309v1", "cs_0501001v1").
func paperStoreID(link string) (string, bool) {
	m := arxivIDRe.FindStringSubmatch(link)
	if m == nil || strings.Contains(m[1], "..") {
		return "", false
	}
	return strings.ReplaceAll(m[1], "/", "_"), true
}

// shouldStorePapers reports whether a freshly parsed paper is saved. pick is true for papers
// the Analyst selected.
func shouldStorePapers(cfg *Config, pick bool) bool {
	return cfg.PaperStoreDir != "" && (cfg.PaperStore == "all" || pick)
}

// expandHome turns a leading "~/" into the user's home directory.
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// loadStoredPaper returns the stored markdown for link (front matter removed), if saved earlier.
func loadStoredPaper(cfg *Config, link string) (string, bool) {
	id, ok := paperStoreID(link)
	if !ok || cfg.PaperStoreDir == "" {
		return "", false
	}
	raw, err := os.ReadFile(filepath.Join(cfg.PaperStoreDir, id+".md"))
	if err != nil {
		return "", false
	}
	md := stripFrontMatter(string(raw))
	return md, strings.TrimSpace(md) != ""
}

func stripFrontMatter(s string) string {
	if !strings.HasPrefix(s, "---\n") {
		return s
	}
	if end := strings.Index(s[4:], "\n---\n"); end >= 0 {
		return strings.TrimLeft(s[4+end+len("\n---\n"):], "\n")
	}
	return s
}

// savePaper writes the markdown (and zip, if any) for link into the store directory.
func savePaper(cfg *Config, link, title string, out MinerUOutput) error {
	id, ok := paperStoreID(link)
	if !ok {
		return fmt.Errorf("not an arXiv link: %s", link)
	}
	if err := os.MkdirAll(cfg.PaperStoreDir, 0o755); err != nil {
		return err
	}
	pdfURL, _ := arxivPDFURL(link)
	front := fmt.Sprintf("---\ntitle: %s\nsource: %s\npdf: %s\nparsed: %s\nparser: MinerU\n---\n\n",
		strconv.Quote(strings.Join(strings.Fields(title), " ")), link, pdfURL, time.Now().Format("2006-01-02"))
	if err := writeFileAtomic(filepath.Join(cfg.PaperStoreDir, id+".md"), []byte(front+out.Markdown)); err != nil {
		return err
	}
	if len(out.Zip) > 0 {
		return writeFileAtomic(filepath.Join(cfg.PaperStoreDir, id+".zip"), out.Zip)
	}
	return nil
}

func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".matcha-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// normalizePaperStore expands ~ in the directory and validates paper_store.
func normalizePaperStore(cfg *Config) {
	cfg.PaperStoreDir = expandHome(strings.TrimSpace(cfg.PaperStoreDir))
	switch strings.ToLower(strings.TrimSpace(cfg.PaperStore)) {
	case "", "picks":
		cfg.PaperStore = "picks"
	case "all":
		cfg.PaperStore = "all"
	default:
		log.Printf("paper_store must be picks or all (got %q); using picks", cfg.PaperStore)
		cfg.PaperStore = "picks"
	}
}
