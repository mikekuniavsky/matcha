package main

import (
	"net/url"
	"regexp"
	"strings"
)

// A heading that starts the back matter: "# References", "## 7 Acknowledgments", "# Appendix A", ...
var backMatterRe = regexp.MustCompile(`(?im)^#{1,6}[ \t]*(?:\d+\.?[ \t]+|[A-Z]\.?[ \t]+)?(?:references?|bibliography|acknowledge?ments?|appendix|appendices|supplementary (?:material|information)|supplemental material)\b.*$`)

// trimPaperText drops the references, acknowledgments and appendices from a paper's
// markdown so the character budget goes to the body and results. It only cuts at a
// heading in the second half of the text, so an early table-of-contents line or a
// mid-paper "see Appendix" heading can't discard the paper.
func trimPaperText(md string) string {
	for _, loc := range backMatterRe.FindAllStringIndex(md, -1) {
		if loc[0] >= len(md)*3/10 {
			return strings.TrimSpace(md[:loc[0]])
		}
	}
	return md
}

var linkRe = regexp.MustCompile(`https?://[^\s)\]>"'<]+`)

// Hosts that usually mean "here is the code or data".
var codeHosts = []string{"github.com", "github.io", "gitlab.com", "huggingface.co", "zenodo.org", "figshare.com", "osf.io", "kaggle.com"}

// extractCodeLinks returns up to max unique code/data/project URLs found in text.
func extractCodeLinks(text string, max int) []string {
	var out []string
	seen := map[string]bool{}
	for _, raw := range linkRe.FindAllString(text, -1) {
		raw = strings.TrimRight(raw, ".,;:!?*_")
		u, err := url.Parse(raw)
		if err != nil || seen[raw] {
			continue
		}
		host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
		for _, h := range codeHosts {
			if host == h || strings.HasSuffix(host, "."+h) {
				seen[raw] = true
				out = append(out, raw)
				break
			}
		}
		if len(out) == max {
			break
		}
	}
	return out
}

// summarizePaperText summarizes a paper's markdown (trimmed of references and
// appendices) and appends any code/data links found in the body.
func summarizePaperText(llm *LLMClient, markdown string) string {
	body := trimPaperText(markdown)
	summary := llm.SummarizePaper(body)
	if summary == "" {
		return ""
	}
	if links := extractCodeLinks(body, 5); len(links) > 0 {
		summary += "\n\n**Code/data:** " + strings.Join(links, " · ")
	}
	return summary
}
