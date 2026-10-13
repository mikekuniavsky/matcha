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

// codeHost describes where a "code or data" link lives. minSegments is how many path
// segments make it a project page rather than a site homepage (github.com/owner/repo).
type codeHost struct {
	host        string
	minSegments int
}

var codeHosts = []codeHost{
	{"github.com", 2}, {"gitlab.com", 2}, {"bitbucket.org", 2}, {"codeberg.org", 2},
	{"huggingface.co", 2}, {"sourceforge.net", 2}, {"pypi.org", 2},
	{"github.io", 0}, {"zenodo.org", 0}, {"figshare.com", 0}, {"osf.io", 0}, {"kaggle.com", 0},
}

// Site sections that look like owner/repo but are not projects (github.com/features/copilot).
var nonProjectSegments = toSet(strings.Fields(`features about pricing login join sponsors orgs topics marketplace settings enterprise
	collections explore issues pulls security site contact customer-stories readme solutions resources apps trending
	papers blog docs search users signup`))

func codeLinkKey(raw string) (key string, ok bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return "", false
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	segs := strings.FieldsFunc(u.Path, func(r rune) bool { return r == '/' })
	for _, h := range codeHosts {
		if h.minSegments == 0 {
			if host == h.host || strings.HasSuffix(host, "."+h.host) {
				return host + "/" + strings.Join(segs, "/"), true
			}
			continue
		}
		if host != h.host {
			continue
		}
		if len(segs) < h.minSegments || nonProjectSegments[strings.ToLower(segs[0])] {
			return "", false
		}
		// One entry per project: owner/repo, ignoring /tree/main, /blob/..., and a trailing .git.
		// Hugging Face prefixes datasets, models and spaces with their kind (datasets/owner/name).
		n := 2
		if host == "huggingface.co" && (segs[0] == "datasets" || segs[0] == "models" || segs[0] == "spaces") && len(segs) >= 3 {
			n = 3
		}
		project := append([]string(nil), segs[:n]...)
		project[n-1] = strings.TrimSuffix(project[n-1], ".git")
		return host + "/" + strings.ToLower(strings.Join(project, "/")), true
	}
	return "", false
}

// extractCodeLinks returns up to max unique code, data and project-page URLs found in text
// (one per distinct project): repository pages, not site homepages.
func extractCodeLinks(text string, max int) []string {
	var out []string
	seen := map[string]bool{}
	for _, raw := range linkRe.FindAllString(text, -1) {
		raw = strings.TrimRight(raw, ".,;:!?*_")
		key, ok := codeLinkKey(raw)
		if !ok || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, raw)
		if len(out) == max {
			break
		}
	}
	return out
}

// articleCodeLinks collects code links from an article's visible text and its link targets.
func articleCodeLinks(a fetchedArticle) []string {
	return extractCodeLinks(a.Text+"\n"+strings.Join(a.Links, "\n"), 5)
}

// addCodeLinks appends a "Code/data" line to a non-empty summary.
func addCodeLinks(summary string, links []string) string {
	if summary == "" || len(links) == 0 {
		return summary
	}
	return summary + "\n\n**Code/data:** " + strings.Join(links, " · ")
}

// summarizePaperText summarizes a paper's markdown (trimmed of references and
// appendices) and appends any code/data links found in the body.
func summarizePaperText(llm *LLMClient, markdown string) string {
	body := trimPaperText(markdown)
	return addCodeLinks(llm.SummarizePaper(body), extractCodeLinks(body, 5))
}
