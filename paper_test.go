package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestTrimPaperText(t *testing.T) {
	body := "# Title\n\n" + strings.Repeat("Body text about CAD generation. ", 40)
	paper := body + "\n\n## 5 Conclusion\n\nWe did it.\n\n## References\n\n[1] Other work https://github.com/other/ref\n\n## Appendix A\n\nExtra.\n"
	got := trimPaperText(paper)
	if !strings.Contains(got, "Conclusion") || strings.Contains(got, "References") || strings.Contains(got, "other/ref") || strings.Contains(got, "Extra") {
		t.Errorf("trimmed wrongly: %q", got[len(got)-80:])
	}

	// An early "Appendix" heading (before 30% of the text) must not truncate the paper.
	early := "# Contents\n\n## Appendix overview\n\n" + strings.Repeat("Real content. ", 100)
	if trimPaperText(early) != early {
		t.Error("cut at an early heading")
	}
	if trimPaperText("no headings here") != "no headings here" {
		t.Error("changed text without back matter")
	}
}

func TestExtractCodeLinks(t *testing.T) {
	text := "Code at https://github.com/foo/bar. Data: (https://huggingface.co/datasets/x/y), again https://github.com/foo/bar, " +
		"site https://example.com/page and https://foo.github.io/proj/."
	got := extractCodeLinks(text, 5)
	want := []string{"https://github.com/foo/bar", "https://huggingface.co/datasets/x/y", "https://foo.github.io/proj/"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
	if got := extractCodeLinks(text, 1); len(got) != 1 {
		t.Errorf("max not honored: %v", got)
	}
}

func TestDedupeKey(t *testing.T) {
	a := dedupeKey("https://arxiv.org/abs/2610.12202v1")
	for _, same := range []string{"http://arxiv.org/abs/2610.12202v2", "https://arxiv.org/pdf/2610.12202", "https://arxiv.org/abs/2610.12202"} {
		if dedupeKey(same) != a {
			t.Errorf("%s keyed differently from %s", same, a)
		}
	}
	if dedupeKey("https://arxiv.org/abs/2610.99999v1") == a {
		t.Error("different papers share a key")
	}
	if dedupeKey("https://example.com/post") != "https://example.com/post" {
		t.Error("non-arXiv link changed")
	}
}

func TestExtractCodeLinksOnlyProjectPages(t *testing.T) {
	text := "Sites: https://github.com/ https://github.com/features/copilot https://github.com/login https://huggingface.co/papers/2401.00001 " +
		"https://www.GitHub.com/Foo/Bar.git and again https://github.com/foo/bar/tree/main, " +
		"model https://huggingface.co/acme/cad-model, data https://huggingface.co/datasets/acme/cad-data and https://huggingface.co/datasets/acme/other-data, repo https://gitlab.com/lab/proj, https://pypi.org/project/cadquery/ " +
		"https://example.com/github.com/foo/baz"
	got := extractCodeLinks(text, 10)
	want := []string{
		"https://www.GitHub.com/Foo/Bar.git", // same project as the later /tree/main link: listed once
		"https://huggingface.co/acme/cad-model",
		"https://huggingface.co/datasets/acme/cad-data",
		"https://huggingface.co/datasets/acme/other-data",
		"https://gitlab.com/lab/proj",
		"https://pypi.org/project/cadquery/",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
}

func TestAddCodeLinks(t *testing.T) {
	if got := addCodeLinks("summary", []string{"https://github.com/a/b", "https://zenodo.org/r/1"}); got != "summary\n\n**Code/data:** https://github.com/a/b · https://zenodo.org/r/1" {
		t.Errorf("got %q", got)
	}
	if addCodeLinks("", []string{"https://github.com/a/b"}) != "" {
		t.Error("links added to an empty summary")
	}
	if addCodeLinks("summary", nil) != "summary" {
		t.Error("summary changed without links")
	}
}

// News articles hide repo links behind anchor text ("the project's GitHub page"); the plain
// text drops the target, so links come from the article's anchors.
func TestFetchArticleKeepsLinks(t *testing.T) {
	para := strings.Repeat("Acme released an open-source CAD generator this week for engineers. ", 12)
	srv := feedServer(t, `<html><body><article><h1>Acme ships CAD tool</h1><p>`+para+`The code is on <a href="https://github.com/acme/cadgen">GitHub</a>
and docs at <a href="/docs">docs</a>, plus <a href="https://example.com/press">the press page</a>.</p><p>`+para+`</p></article></body></html>`)
	a, err := fetchArticle(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if got := articleCodeLinks(a); !reflect.DeepEqual(got, []string{"https://github.com/acme/cadgen"}) {
		t.Errorf("code links = %v (all links %v)", got, a.Links)
	}
}
