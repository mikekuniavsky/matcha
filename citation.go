package main

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/spf13/viper"
)

// CitationWatch is one entry of `citation_watch` in config.yaml: papers whose new citations you
// want to follow. Matcha asks OpenAlex for works that cite any of `papers` and treats the result
// like an arXiv search: listed (use: feed or summary) or screened by an analyst (use: analyst).
type CitationWatch struct {
	Name    string   `mapstructure:"name"`
	Use     string   `mapstructure:"use"`     // analyst (default) | summary | feed
	Days    int      `mapstructure:"days"`    // look back this many days by publication date (default 30)
	Limit   int      `mapstructure:"limit"`   // newest N citing papers kept
	Papers  []string `mapstructure:"papers"`  // arXiv IDs ("arXiv:2509.15246"), DOIs or OpenAlex IDs ("W123")
	Analyst string   `mapstructure:"analyst"` // use: analyst only: which analyst reads it (default the first)
}

// loadCitationWatch expands `citation_watch` into feeds and analyst feeds, like loadArxivSearches.
func loadCitationWatch(cfg *Config) (feeds []RSS, analyst []arxivAnalystFeed) {
	var watches []CitationWatch
	if err := viper.UnmarshalKey("citation_watch", &watches); err != nil {
		log.Printf("citation_watch: %v", err)
		return nil, nil
	}
	cfg.CitationWatch = watches
	for i, w := range watches {
		name := strings.TrimSpace(w.Name)
		if name == "" {
			name = fmt.Sprintf("#%d", i+1)
		}
		var papers []string
		for _, p := range w.Papers {
			if p = strings.TrimSpace(p); p == "" {
				continue
			}
			if _, err := refFilter(p); err != nil {
				log.Printf("citation_watch %s: %v (skipped)", name, err)
				continue
			}
			papers = append(papers, p)
		}
		if len(papers) == 0 {
			log.Printf("citation_watch %s: no usable papers (skipped)", name)
			continue
		}
		use := strings.ToLower(strings.TrimSpace(w.Use))
		if use == "" {
			use = "analyst"
		}
		if w.Limit <= 0 {
			w.Limit = map[string]int{"summary": 5, "analyst": 50, "feed": 30}[use]
		}
		if w.Days <= 0 {
			w.Days = 30
		}
		feedURL := citationFeedURL(name, papers, w.Days)
		switch use {
		case "summary":
			feeds = append(feeds, RSS{url: feedURL, limit: w.Limit, summarize: true, name: "Cites: " + name})
		case "feed":
			feeds = append(feeds, RSS{url: feedURL, limit: w.Limit, name: "Cites: " + name})
		case "analyst":
			analyst = append(analyst, arxivAnalystFeed{kind: "citation_watch", name: name, analyst: w.Analyst, spec: fmt.Sprintf("%s %d", feedURL, w.Limit)})
		default:
			log.Printf("citation_watch %s: use must be summary, analyst or feed (got %q) (skipped)", name, w.Use)
		}
	}
	return feeds, analyst
}

// storedPaperRefs lists the papers saved in paper_store_dir as arXiv references.
func storedPaperRefs(dir string) []string {
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var refs []string
	for _, e := range entries {
		id := strings.TrimSuffix(e.Name(), ".md")
		if id == e.Name() || strings.HasPrefix(id, ".") {
			continue
		}
		id = arxivVersionRe.ReplaceAllString(strings.Replace(id, "_", "/", 1), "")
		if _, err := refFilter("arXiv:" + id); err == nil {
			refs = append(refs, "arXiv:"+id)
		}
	}
	return refs
}

type anchorCount struct {
	id    string
	count int
}

// runAnchors finds the papers that your papers cite in common: the watch papers plus any papers
// saved in paper_store_dir. Those shared references are the natural papers to put under
// `citation_watch`, since new work in the same area tends to cite them too.
func runAnchors(cfg *Config) {
	var refs []string
	seen := map[string]bool{}
	add := func(r string) {
		k := strings.ToLower(strings.TrimSpace(r))
		if k != "" && !seen[k] {
			seen[k] = true
			refs = append(refs, r)
		}
	}
	for _, w := range cfg.CitationWatch {
		for _, p := range w.Papers {
			add(p)
		}
	}
	for _, r := range storedPaperRefs(cfg.PaperStoreDir) {
		add(r)
	}
	if len(refs) < 2 {
		fmt.Println("-anchors needs at least two papers: list them under citation_watch (papers:) or save some with paper_store_dir.")
		return
	}

	seedIDs := map[string]bool{}
	var seeds []oaWork
	for _, r := range refs {
		w, err := resolveWork(r)
		if err != nil {
			fmt.Printf("skipped %s: %v\n", r, err)
			continue
		}
		if !seedIDs[w.shortID()] {
			seedIDs[w.shortID()] = true
			seeds = append(seeds, w)
		}
	}

	counts := map[string]int{}
	withRefs := 0
	for _, w := range seeds {
		if len(w.ReferencedWorks) > 0 {
			withRefs++
		}
		dup := map[string]bool{}
		for _, ref := range w.ReferencedWorks {
			id := ref[strings.LastIndex(ref, "/")+1:]
			if !seedIDs[id] && !dup[id] {
				dup[id] = true
				counts[id]++
			}
		}
	}
	fmt.Printf("%d papers found in OpenAlex, %d with a reference list.\n", len(seeds), withRefs)
	if withRefs < len(seeds) {
		fmt.Println("(New preprints often have no reference list in OpenAlex for days or weeks; their citations are not counted yet.)")
	}

	minCount := 2
	if withRefs < 2 {
		minCount = 1
	}
	var ranked []anchorCount
	for id, n := range counts {
		if n >= minCount {
			ranked = append(ranked, anchorCount{id, n})
		}
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].count != ranked[j].count {
			return ranked[i].count > ranked[j].count
		}
		return ranked[i].id < ranked[j].id
	})
	if len(ranked) > 40 {
		ranked = ranked[:40]
	}
	if len(ranked) == 0 {
		fmt.Println("No paper is cited by more than one of them.")
		return
	}

	var ids []string
	for _, a := range ranked {
		ids = append(ids, a.id)
	}
	var list oaList
	if _, err := openAlexGet("/works", url.Values{"filter": {"openalex:" + strings.Join(ids, "|")}, "per-page": {"50"}, "select": {oaSelectLight}}, &list); err != nil {
		fmt.Printf("Could not look up the shared references: %v\n", err)
		return
	}
	byID := map[string]oaWork{}
	for _, w := range list.Results {
		byID[w.shortID()] = w
	}

	fmt.Printf("\nCited most often by your %d papers:\n", withRefs)
	var yaml []string
	for _, a := range ranked {
		w, ok := byID[a.id]
		label, ref := a.id, a.id
		if ok {
			label = fmt.Sprintf("%s (%d, cited %d times)", w.name(), w.PublicationYear, w.CitedByCount)
			if id := w.arxivID(); id != "" {
				ref = "arXiv:" + id
			}
		}
		fmt.Printf("  %d of %d  %s  [%s]\n", a.count, withRefs, label, ref)
		if len(yaml) < 10 {
			yaml = append(yaml, fmt.Sprintf("      - %s   # %s", ref, shorten(strings.SplitN(label, " (", 2)[0], 70)))
		}
	}
	fmt.Println("\nTo follow new papers that cite the top ones, add them to citation_watch:")
	fmt.Println("  citation_watch:\n    - name: Anchors\n      papers:\n" + strings.Join(yaml, "\n"))
}
