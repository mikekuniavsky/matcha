package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPaperStoreID(t *testing.T) {
	for in, want := range map[string]string{
		"https://arxiv.org/abs/2610.13309v1":     "2610.13309v1",
		"http://arxiv.org/abs/2610.13309":        "2610.13309",
		"https://arxiv.org/abs/cs/0501001v2":     "cs_0501001v2",
		"https://arxiv.org/pdf/2610.13309v1.pdf": "2610.13309v1",
	} {
		if got, ok := paperStoreID(in); !ok || got != want {
			t.Errorf("paperStoreID(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	if _, ok := paperStoreID("https://example.com/paper.pdf"); ok {
		t.Error("non-arXiv link accepted")
	}
}

func TestShouldStorePapers(t *testing.T) {
	cases := []struct {
		dir, mode string
		pick      bool
		want      bool
	}{
		{"", "all", true, false},      // no directory: nothing is saved
		{"/d", "picks", true, true},   // Analyst pick
		{"/d", "picks", false, false}, // feed hit, picks mode
		{"/d", "all", false, true},    // feed hit, all mode
	}
	for _, c := range cases {
		if got := shouldStorePapers(&Config{PaperStoreDir: c.dir, PaperStore: c.mode}, c.pick); got != c.want {
			t.Errorf("%+v: got %v", c, got)
		}
	}
}

func TestNormalizePaperStore(t *testing.T) {
	home, _ := os.UserHomeDir()
	cfg := &Config{PaperStoreDir: "~/papers", PaperStore: "ALL"}
	normalizePaperStore(cfg)
	if cfg.PaperStoreDir != filepath.Join(home, "papers") || cfg.PaperStore != "all" {
		t.Errorf("%+v", cfg)
	}
	cfg = &Config{PaperStore: "nonsense"}
	normalizePaperStore(cfg)
	if cfg.PaperStore != "picks" {
		t.Errorf("bad value not reset: %+v", cfg)
	}
}

func TestSaveAndLoadPaper(t *testing.T) {
	cfg := &Config{PaperStoreDir: filepath.Join(t.TempDir(), "sub", "papers")}
	link := "https://arxiv.org/abs/2610.13309v1"
	if _, ok := loadStoredPaper(cfg, link); ok {
		t.Fatal("found a paper before saving")
	}
	if err := savePaper(cfg, link, "A \"quoted\"\n title", MinerUOutput{Markdown: "# Body\n\ntext --- not front matter\n", Zip: []byte("Z")}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(cfg.PaperStoreDir, "2610.13309v1.md"))
	for _, want := range []string{"---\ntitle: \"A \\\"quoted\\\" title\"\n", "source: " + link, "pdf: https://arxiv.org/pdf/2610.13309v1", "parser: MinerU\n---\n\n# Body"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("saved file missing %q:\n%s", want, raw)
		}
	}
	if zip, _ := os.ReadFile(filepath.Join(cfg.PaperStoreDir, "2610.13309v1.zip")); string(zip) != "Z" {
		t.Errorf("zip = %q", zip)
	}
	md, ok := loadStoredPaper(cfg, link)
	if !ok || md != "# Body\n\ntext --- not front matter\n" {
		t.Errorf("loaded %q, %v", md, ok)
	}
}

// End to end against the fake MinerU: parse and save, then reuse the stored copy without MinerU.
func TestGetPaperMarkdownStoresAndReuses(t *testing.T) {
	oldPoll, oldDownload := mineruPollInterval, downloadPDF
	mineruPollInterval = time.Millisecond
	downloadPDF = func(string) ([]byte, error) { return []byte("%PDF-1.4 fake"), nil }
	defer func() { mineruPollInterval, downloadPDF = oldPoll, oldDownload }()
	var urlJobs int
	srv := fakeMinerU(t, true, &urlJobs)
	defer func() {
		if urlJobs != 0 {
			t.Errorf("arXiv papers must not use URL jobs (MinerU can't fetch them), got %d", urlJobs)
		}
	}()
	cfg := &Config{MinerUURL: srv.URL, MinerUTier: "basic", PaperStoreDir: t.TempDir(), PaperStore: "picks", PaperStoreZip: true}
	link := "https://arxiv.org/abs/2610.13309v1"

	if md := getPaperMarkdown(link, "Some Paper", cfg, false); md != "# Parsed paper" { // a feed hit: parsed but not saved in picks mode
		t.Fatalf("feed hit: %q", md)
	}
	if entries, _ := os.ReadDir(cfg.PaperStoreDir); len(entries) != 0 {
		t.Fatalf("saved a non-pick in picks mode: %v", entries)
	}

	if md := getPaperMarkdown(link, "Some Paper", cfg, true); md != "# Parsed paper" {
		t.Fatalf("pick: %q", md)
	}
	if zip, _ := os.ReadFile(filepath.Join(cfg.PaperStoreDir, "2610.13309v1.zip")); string(zip) != "ZIPBYTES" {
		t.Errorf("zip = %q", zip)
	}
	jobsBefore := urlJobs

	srv.Close() // MinerU gone: the stored copy must be enough
	if md := getPaperMarkdown(link, "Some Paper", cfg, false); md != "# Parsed paper" || urlJobs != jobsBefore {
		t.Errorf("stored copy not reused: %q", md)
	}
}
