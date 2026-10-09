package main

import (
	"html"
	"regexp"
	"strings"
)

// Near-duplicate story detection for titles in one feed (e.g. five outlets covering the
// same announcement). The goal is to summarize one story per cluster, not to merge
// merely related stories, so the rules are deliberately strict:
//   - words that appear in half or more of the feed's titles ("CAD" in a CAD alert,
//     a person's name in a person-specific alert) are ignored for matching;
//   - two titles are the same story only if they share at least minSharedWords of the
//     remaining words AND those make up at least minOverlap of all their remaining words.
const (
	minSharedWords = 3
	minOverlap     = 0.4
	genericShare   = 0.5 // a word in this share of titles is "generic" for that feed
	minTitlesForDF = 4   // below this, a feed is too small to judge which words are generic
)

var (
	outletSuffixRe = regexp.MustCompile(`\s[-|–—]\s[^-|–—]{1,60}$`) // "Headline - Publisher"
	nonWordRe      = regexp.MustCompile(`[^a-z0-9]+`)
	stopwords      = toSet(strings.Fields(`a an and are as at be but by for from has have how in into is it its of on or over that the their this to was were what when where which who why will with
		says say said new could can may might just your you our us after before about more not than then there these those`))
)

func toSet(words []string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

// titleWords lowercases a title, drops the trailing outlet name, HTML and punctuation,
// removes stopwords, and lightly stems plurals ("days" -> "day").
func titleWords(title string) []string {
	t := html.UnescapeString(stripHtmlRegex(title))
	t = outletSuffixRe.ReplaceAllString(strings.TrimSpace(t), "")
	var words []string
	for _, w := range nonWordRe.Split(strings.ToLower(t), -1) {
		if w == "" || stopwords[w] {
			continue
		}
		if len(w) > 3 && strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") {
			w = w[:len(w)-1]
		}
		if len(w) == 1 && (w[0] < '0' || w[0] > '9') { // keep digits like "3"; drop stray letters
			continue
		}
		words = append(words, w)
	}
	return words
}

// clusterSimilarTitles returns rep[i]: the index of the first title that item i duplicates,
// or i itself when it starts its own cluster.
func clusterSimilarTitles(titles []string) []int {
	n := len(titles)
	sets := make([]map[string]bool, n)
	df := map[string]int{}
	for i, t := range titles {
		sets[i] = toSet(titleWords(t))
		for w := range sets[i] {
			df[w]++
		}
	}
	if n >= minTitlesForDF {
		for i := range sets {
			for w := range sets[i] {
				if float64(df[w])/float64(n) >= genericShare {
					delete(sets[i], w)
				}
			}
		}
	}

	rep := make([]int, n)
	for i := range rep {
		rep[i] = i
		for j := 0; j < i; j++ {
			if rep[j] != j { // compare only against cluster starters
				continue
			}
			if sameStory(sets[i], sets[j]) {
				rep[i] = j
				break
			}
		}
	}
	return rep
}

func sameStory(a, b map[string]bool) bool {
	shared := 0
	for w := range a {
		if b[w] {
			shared++
		}
	}
	union := len(a) + len(b) - shared
	return shared >= minSharedWords && union > 0 && float64(shared)/float64(union) >= minOverlap
}
