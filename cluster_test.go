package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mmcdole/gofeed"
)

// Titles from a real Google Alert digest: the same few events covered by many outlets.
var bezosTitles = []string{
	"Jeff <b>Bezos</b> says AI could result in a 3-day work week",
	"Jeff <b>Bezos</b> reveals AI vision that could transform the American workweek",
	"Jeff <b>Bezos</b> Claims AI Can Shorten American Workweek to 3 Days: &#39;People Don&#39;t Want to Work 2 Jobs&#39;",
	"AI may let people work just 3 days a week, says Amazon founder Jeff <b>Bezos</b> - Digit",
	"Amazon founder Jeff <b>Bezos</b> says AI could let people work just 3 days a week - Firstpost",
	"<b>Bezos</b> says Blue Origin likely to pursue IPO in coming years - Reuters",
	"Jeff <b>Bezos</b> says Blue Origin likely to pursue IPO in coming years - The Economic Times",
	"Jeff <b>Bezos</b> Takes Aim at Politicians &#39;Picking Villains&#39; Over Housing Costs as Socialism Debate Grows",
	"How Jeff <b>Bezos</b> added $102 billion to his fortune in one month - TheStreet",
	"Jeff <b>Bezos</b> calls Mars a destination for humans, but wants to take Americans to Moon first",
}

func TestClusterSimilarTitles(t *testing.T) {
	rep := clusterSimilarTitles(bezosTitles)

	// "3 days a week" stories (0, 3, 4) are one cluster; the two IPO headlines (5, 6) are another.
	if rep[3] != rep[0] || rep[4] != rep[0] {
		t.Errorf("'3 days a week' headlines should cluster: rep=%v", rep)
	}
	if rep[6] != rep[5] || rep[5] == rep[0] {
		t.Errorf("IPO headlines should form their own cluster: rep=%v", rep)
	}
	// Distinct stories stay separate, even though every title names the same person.
	for _, i := range []int{7, 8, 9} {
		if rep[i] != i {
			t.Errorf("title %d wrongly merged into %d: %q", i, rep[i], bezosTitles[i])
		}
	}
}

// A topic keyword shared by every title must not merge different stories.
func TestClusterDoesNotOverIndexOnTopicWord(t *testing.T) {
	cad := []string{
		"MecAgent targets complex CAD tasks with GPT-6 Astra powered V2 - DEVELOP3D",
		"Backflip AI Converts 2D Engineering Drawings and Photos into 3D CAD - Yahoo Finance",
		"OwlCAD brings parametric CAD and AI-powered design to the browser - VoxelMatters",
		"CADProps: Open, measure, and compare CAD files in your browser - 3Druck.com",
		"Neural4D Upgrades Coala AI CAD Agent with Deep Thinking and Drawing to CAD",
		"Mark3D becomes Backflip AI's first global reseller partner - Manufacturing Management",
		"Silver Elephant Mining Corp. Ups Placement to CAD 882K - TradingView",
	}
	for i, r := range clusterSimilarTitles(cad) {
		if r != i {
			t.Errorf("CAD title %d merged into %d: %q", i, r, cad[i])
		}
	}
	// A tiny feed (no word statistics) still needs 3 shared words.
	if r := clusterSimilarTitles([]string{"AI CAD tool launched today", "New AI CAD startup raises funds"}); r[1] != 1 {
		t.Errorf("two titles sharing only 'AI CAD' were merged: %v", r)
	}
}

func TestTitleWords(t *testing.T) {
	got := titleWords("Jeff <b>Bezos</b>&#39;s AI can let people work 3 days - Digit")
	want := []string{"jeff", "bezo", "ai", "let", "people", "work", "3", "day"}
	// "bezos's" splits to "bezos" + "s": stemming and the 1-letter drop apply
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got %v want %v", got, want)
			break
		}
	}
}

type captureWriter struct{ out string }

func (c *captureWriter) Write(body string) { c.out += body }
func (c *captureWriter) WriteLink(title, url string, nl bool, rt string) string {
	return "LINK[" + title + "]\n"
}
func (c *captureWriter) WriteSummary(content string, nl bool) string {
	if content == "" {
		return ""
	}
	return "SUM{" + content + "}\n"
}
func (c *captureWriter) WriteHeader(feed *gofeed.Feed) string { return "HEADER[" + feed.Title + "]\n" }

// Two outlets covering one story and one unrelated story: the duplicate is listed but
// not summarized, and the unrelated story keeps its own summary.
func TestProcessFeedSummarizesOneStoryPerCluster(t *testing.T) {
	const rss = `<?xml version="1.0"?><rss version="2.0"><channel><title>Alert</title><link>https://alert.example</link>
<item><title>Blue Origin likely to pursue IPO in coming years - Reuters</title><link>https://a.example/1</link><description>Blue Origin IPO story told by Reuters in some detail so the description is long enough.</description></item>
<item><title>Bezos says Blue Origin likely to pursue IPO in coming years - Economic Times</title><link>https://b.example/2</link><description>The same IPO story told by another outlet, also with a reasonably long description.</description></item>
<item><title>Backflip AI launches drawing to CAD</title><link>https://c.example/3</link><description>A different story about drawings turning into CAD models, with its own long description.</description></item>
</channel></rss>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(rss)) }))
	defer srv.Close()

	store, err := NewStorage(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	shownThisRun = map[string]bool{}
	w := &captureWriter{}
	cfg := &Config{ClusterSimilarStories: true}
	ProcessFeed(RSS{url: srv.URL, limit: 10, summarize: true}, cfg, store, nil, w, gofeed.NewParser())

	if n := strings.Count(w.out, "SUM{") - strings.Count(w.out, "SUM{↳"); n != 2 { // first IPO story + the unrelated one
		t.Errorf("want 2 summaries, got %d:\n%s", n, w.out)
	}
	if !strings.Contains(w.out, "Same story as: Blue Origin likely to pursue IPO in coming years - Reuters") {
		t.Errorf("duplicate not annotated:\n%s", w.out)
	}
	if !strings.Contains(w.out, "LINK[Bezos says Blue Origin") {
		t.Errorf("duplicate link should still be listed:\n%s", w.out)
	}
}
