package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
)

// OpenAlex (https://openalex.org) is a free index of scholarly works and their citations. arXiv has no
// "cited by" lookup, so a citation watch asks OpenAlex for papers that cite chosen papers.
//
// A watch is exposed to the rest of Matcha as a feed URL with the scheme openalex-cites:, which
// fetchFeed answers by querying OpenAlex. That way a watch works wherever a feed does: listed
// sections, analyst input, `-check-feeds`, retries and pacing.

const citeScheme = "openalex-cites:"

var (
	// Overridable: tests point openAlexBase at a local server; LoadConfig sets email and key.
	openAlexBase  = "https://api.openalex.org"
	openAlexEmail = "" // optional: OpenAlex asks API users to identify themselves (the "polite pool")
	openAlexKey   = "" // optional

	resolvedWorks = map[string]oaWork{} // seed reference -> work, for this run
)

func init() {
	minHostInterval["api.openalex.org"] = 150 * time.Millisecond // OpenAlex allows about 10 requests a second
}

type oaLocation struct {
	LandingPageURL string `json:"landing_page_url"`
	PDFURL         string `json:"pdf_url"`
}

type oaWork struct {
	ID                    string           `json:"id"`
	DOI                   string           `json:"doi"`
	DisplayName           string           `json:"display_name"`
	Title                 string           `json:"title"`
	PublicationDate       string           `json:"publication_date"`
	PublicationYear       int              `json:"publication_year"`
	CitedByCount          int              `json:"cited_by_count"`
	AbstractInvertedIndex map[string][]int `json:"abstract_inverted_index"`
	PrimaryLocation       *oaLocation      `json:"primary_location"`
	Locations             []oaLocation     `json:"locations"`
}

type oaList struct {
	Results []oaWork `json:"results"`
}

// shortID turns "https://openalex.org/W123" into "W123".
func (w oaWork) shortID() string {
	return w.ID[strings.LastIndex(w.ID, "/")+1:]
}

func (w oaWork) name() string {
	for _, t := range []string{w.DisplayName, w.Title} {
		if t = strings.Join(strings.Fields(t), " "); t != "" {
			return t
		}
	}
	return w.shortID()
}

var arxivDOIRe = regexp.MustCompile(`(?i)10\.48550/arxiv\.(.+)$`)

// arxivID finds the arXiv ID of a work from its locations or its arXiv-issued DOI, or "".
func (w oaWork) arxivID() string {
	var urls []string
	if w.PrimaryLocation != nil {
		urls = append(urls, w.PrimaryLocation.LandingPageURL, w.PrimaryLocation.PDFURL)
	}
	for _, l := range w.Locations {
		urls = append(urls, l.LandingPageURL, l.PDFURL)
	}
	for _, u := range urls {
		if m := arxivIDRe.FindStringSubmatch(u); m != nil {
			return arxivVersionRe.ReplaceAllString(m[1], "")
		}
	}
	if m := arxivDOIRe.FindStringSubmatch(w.DOI); m != nil {
		return arxivVersionRe.ReplaceAllString(m[1], "")
	}
	return ""
}

// link prefers the arXiv page (so the paper can be summarized from its PDF), then the DOI.
func (w oaWork) link() string {
	if id := w.arxivID(); id != "" {
		return "https://arxiv.org/abs/" + id
	}
	if w.DOI != "" {
		return w.DOI
	}
	return w.ID
}

// abstract rebuilds the abstract from OpenAlex's inverted index (word -> positions).
func (w oaWork) abstract() string {
	if len(w.AbstractInvertedIndex) == 0 {
		return ""
	}
	var words []string
	for word, positions := range w.AbstractInvertedIndex {
		for _, p := range positions {
			for len(words) <= p {
				words = append(words, "")
			}
			words[p] = word
		}
	}
	return strings.Join(strings.Fields(strings.Join(words, " ")), " ")
}

const (
	oaSelectLight = "id,doi,display_name,publication_year,cited_by_count,primary_location,locations"
	oaSelectFull  = "id,doi,display_name,publication_date,publication_year,cited_by_count,primary_location,locations,abstract_inverted_index"
)

// openAlexGet queries OpenAlex, waiting and retrying on 429 and 503 like fetchFeed does.
func openAlexGet(path string, q url.Values, out interface{}) (feedResult, error) {
	var res feedResult
	if openAlexEmail != "" {
		q.Set("mailto", openAlexEmail)
	}
	if openAlexKey != "" {
		q.Set("api_key", openAlexKey)
	}
	full := openAlexBase + path + "?" + q.Encode()
	base, err := url.Parse(openAlexBase)
	if err != nil {
		return res, err
	}
	for attempt := 0; ; attempt++ {
		politeWait(base.Hostname())
		req, err := http.NewRequest(http.MethodGet, full, nil)
		if err != nil {
			return res, err
		}
		req.Header.Set("User-Agent", feedUA)
		resp, err := feedClient.Do(req)
		res.Attempts++
		if err != nil {
			return res, err
		}
		res.Status = resp.StatusCode
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
		resp.Body.Close()

		if (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable) && attempt < len(retryDelays) {
			wait, ok := retryAfter(resp.Header.Get("Retry-After"))
			if !ok {
				wait = retryDelays[attempt]
			}
			fmt.Printf("  %s answered %s; waiting %s before retry %d of %d\n", base.Hostname(), resp.Status, wait.Round(time.Second), attempt+1, len(retryDelays))
			sleepFor(wait)
			continue
		}
		if resp.StatusCode >= 400 {
			return res, fmt.Errorf("openalex http error: %s: %.200s", resp.Status, body)
		}
		if readErr != nil {
			return res, readErr
		}
		if err := json.Unmarshal(body, out); err != nil {
			return res, fmt.Errorf("openalex: bad json: %w", err)
		}
		return res, nil
	}
}

var (
	arxivRefRe = regexp.MustCompile(`(?i)^(?:arxiv:|https?://(?:www\.)?arxiv\.org/(?:abs|pdf)/)?([0-9]{4}\.[0-9]{4,5}|[a-z-]+(?:\.[a-z]{2})?/[0-9]{7})(?:v[0-9]+)?(?:\.pdf)?$`)
	oaIDRe     = regexp.MustCompile(`(?i)^(?:https?://openalex\.org/)?(W[0-9]+)$`)
	doiRefRe   = regexp.MustCompile(`(?i)^(?:https?://(?:dx\.)?doi\.org/|doi:)?(10\.[0-9]{4,9}/\S+)$`)
)

// refFilter turns a paper reference (arXiv ID, DOI or OpenAlex ID) into an OpenAlex filter.
func refFilter(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	switch {
	case oaIDRe.MatchString(ref):
		return "openalex:" + strings.ToUpper(oaIDRe.FindStringSubmatch(ref)[1]), nil
	case arxivRefRe.MatchString(ref):
		// arXiv papers carry a DataCite DOI, 10.48550/arXiv.<id>
		return "doi:10.48550/arxiv." + strings.ToLower(arxivRefRe.FindStringSubmatch(ref)[1]), nil
	case doiRefRe.MatchString(ref):
		return "doi:" + doiRefRe.FindStringSubmatch(ref)[1], nil
	}
	return "", fmt.Errorf("%q is not an arXiv ID (arXiv:2509.15246), DOI or OpenAlex ID (W...)", ref)
}

// resolveWork finds one paper in OpenAlex.
func resolveWork(ref string) (oaWork, error) {
	if w, ok := resolvedWorks[ref]; ok {
		return w, nil
	}
	filter, err := refFilter(ref)
	if err != nil {
		return oaWork{}, err
	}
	var list oaList
	if _, err := openAlexGet("/works", url.Values{"filter": {filter}, "per-page": {"1"}, "select": {oaSelectLight}}, &list); err != nil {
		return oaWork{}, err
	}
	if len(list.Results) == 0 {
		return oaWork{}, fmt.Errorf("OpenAlex has no record of %s yet (new preprints can take days to appear)", ref)
	}
	resolvedWorks[ref] = list.Results[0]
	return list.Results[0], nil
}

// citationFeedURL builds the pseudo feed URL for a citation watch.
func citationFeedURL(name string, refs []string, days int) string {
	q := url.Values{"name": {name}, "days": {strconv.Itoa(days)}}
	for _, r := range refs {
		q.Add("ref", strings.TrimSpace(r))
	}
	return citeScheme + "//watch?" + q.Encode()
}

// fetchOpenAlexCites answers an openalex-cites: feed URL: works published in the last `days`
// days that cite any of the `ref` papers, newest first, as a feed.
func fetchOpenAlexCites(feedURL string) (*gofeed.Feed, feedResult, error) {
	var res feedResult
	u, err := url.Parse(feedURL)
	if err != nil {
		return nil, res, err
	}
	q := u.Query()
	days, _ := strconv.Atoi(q.Get("days"))
	if days <= 0 {
		days = 30
	}

	seeds := map[string]bool{}
	var ids []string
	var firstErr error
	for _, ref := range q["ref"] {
		w, err := resolveWork(ref)
		if err != nil {
			log.Printf("Citation watch %q: %v", q.Get("name"), err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if !seeds[w.shortID()] {
			seeds[w.shortID()] = true
			ids = append(ids, w.shortID())
		}
	}
	if len(ids) == 0 {
		if firstErr == nil {
			firstErr = fmt.Errorf("no papers listed")
		}
		return nil, res, fmt.Errorf("none of the papers to watch could be found: %w", firstErr)
	}
	if len(ids) > 100 { // OpenAlex accepts at most 100 values in one filter
		ids = ids[:100]
	}

	since := time.Now().AddDate(0, 0, -days).Format("2006-01-02")
	var list oaList
	res, err = openAlexGet("/works", url.Values{
		"filter":   {"cites:" + strings.Join(ids, "|") + ",from_publication_date:" + since},
		"per-page": {"200"},
		"select":   {oaSelectFull},
	}, &list)
	if err != nil {
		return nil, res, err
	}

	works := list.Results[:0]
	for _, w := range list.Results {
		if !seeds[w.shortID()] { // a seed that cites another seed is not news
			works = append(works, w)
		}
	}
	sort.SliceStable(works, func(i, j int) bool { return works[i].PublicationDate > works[j].PublicationDate })

	distinct := map[string]bool{}
	for _, ref := range q["ref"] {
		distinct[strings.ToLower(strings.TrimSpace(ref))] = true
	}
	fmt.Printf("  OpenAlex: found %d of %d watched papers; %d papers cite them from the last %d days\n", len(ids), len(distinct), len(works), days)

	feed := &gofeed.Feed{Title: "Cites: " + q.Get("name"), Link: "https://openalex.org", FeedLink: "https://openalex.org"}
	for _, w := range works {
		item := &gofeed.Item{Title: w.name(), Link: w.link(), GUID: w.link(), Description: w.abstract(), Published: w.PublicationDate}
		if t, err := time.Parse("2006-01-02", w.PublicationDate); err == nil {
			item.PublishedParsed = &t
		}
		feed.Items = append(feed.Items, item)
	}
	return feed, res, nil
}
