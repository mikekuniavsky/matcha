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
