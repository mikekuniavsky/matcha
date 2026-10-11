package main

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	readability "github.com/go-shiori/go-readability"
	"github.com/mmcdole/gofeed"
)

// shownThisRun holds the dedupe keys of items already written to this digest, so a paper
// the Analyst summarized (or another feed already listed) isn't printed a second time.
var shownThisRun = map[string]bool{}

var arxivVersionRe = regexp.MustCompile(`v\d+$`)

// dedupeKey identifies an item across feeds; arXiv links ignore the version suffix.
func dedupeKey(link string) string {
	if m := arxivIDRe.FindStringSubmatch(link); m != nil {
		return "arxiv:" + arxivVersionRe.ReplaceAllString(m[1], "")
	}
	return link
}

// findSimilarStories clusters the titles of the feed's not-yet-shown items and returns,
// for each later duplicate, the title of the first story in its cluster.
func findSimilarStories(items []*gofeed.Item, store *Storage) map[string]string {
	var cands []*gofeed.Item
	for _, it := range items {
		if shownThisRun[dedupeKey(it.Link)] {
			continue
		}
		if seen, _, _ := store.IsSeen(it.Link); seen {
			continue // shown on an earlier day: not part of today's output
		}
		cands = append(cands, it)
	}
	titles := make([]string, len(cands))
	for i, it := range cands {
		titles[i] = it.Title
	}
	dup := map[string]string{}
	for i, r := range clusterSimilarTitles(titles) {
		if r != i {
			dup[cands[i].Link] = cands[r].Title
		}
	}
	return dup
}

// placeholderFeed stands in for a feed that could not be read, so its section still gets a header.
func placeholderFeed(rss RSS) *gofeed.Feed {
	title := rss.name
	if title == "" {
		title = shorten(strings.TrimPrefix(strings.TrimPrefix(rss.url, "https://"), "http://"), 70)
	}
	return &gofeed.Feed{Title: title, FeedLink: rss.url}
}

func shorten(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > max {
		return s[:max-1] + "…"
	}
	return s
}

// emptyFeedNote explains why a section has nothing to show, so an empty section is
// distinguishable from one that never ran.
func emptyFeedNote(feedURL string, total, alreadySeen, shownAbove int) string {
	if total == 0 {
		note := "_No items returned by this feed._"
		if strings.Contains(feedURL, "rss.arxiv.org") {
			note += " _arXiv's RSS feeds are empty on days without announcements (weekends, holidays)._"
		}
		return note
	}
	var parts []string
	if alreadySeen > 0 {
		parts = append(parts, fmt.Sprintf("%d already seen on earlier days", alreadySeen))
	}
	if shownAbove > 0 {
		parts = append(parts, fmt.Sprintf("%d already shown above in this digest", shownAbove))
	}
	return fmt.Sprintf("_No new items: %d read, %s._", total, strings.Join(parts, ", "))
}

func ProcessFeed(rss RSS, cfg *Config, store *Storage, llm *LLMClient, w Writer, fp *gofeed.Parser) {
	feed, err := fp.ParseURL(rss.url)
	if err != nil {
		log.Printf("Error parsing %s: %v", rss.url, err)
		if cfg.ShowEmptySections {
			w.Write(w.WriteHeader(placeholderFeed(rss)) + w.WriteSummary("⚠️ Could not read this feed: "+shorten(err.Error(), 160), true))
		}
		return
	}
	if rss.name != "" {
		feed.Title = rss.name
	}

	if len(feed.Items) > rss.limit {
		feed.Items = feed.Items[:rss.limit]
	}

	var outputBuffer string
	itemsFound := false
	total, alreadySeen, shownAbove, similar := len(feed.Items), 0, 0, 0

	// Near-duplicate stories (the same news from several outlets) are summarized once.
	var similarTo map[string]string // item link -> title of the story it duplicates
	if rss.summarize && cfg.ClusterSimilarStories {
		similarTo = findSimilarStories(feed.Items, store)
	}

	for _, item := range feed.Items {
		if shownThisRun[dedupeKey(item.Link)] {
			shownAbove++
			continue
		}

		// Check DB for seen status
		seen, seenToday, prevSummary := store.IsSeen(item.Link)

		// If we've seen it before (and not today), skip it
		if seen {
			alreadySeen++
			continue
		}

		itemsFound = true
		shownThisRun[dedupeKey(item.Link)] = true
		title := item.Title
		if title == "" {
			title = stripHtmlRegex(item.Description)
		}

		summary := prevSummary
		repTitle, isSimilar := similarTo[item.Link]
		if summary == "" && rss.summarize && !isSimilar {
			summary = getSummary(llm, item, cfg)
		}

		var readingTime string
		if cfg.ReadingTime {
			readingTime = getReadingTime(item.Link)
		}

		if strings.Contains(feed.Link, "news.ycombinator.com") {
			outputBuffer += formatHackerNewsLinks(w, item)
		}

		if cfg.Instapaper && !cfg.TerminalMode {
			outputBuffer += getInstapaperLink(item.Link)
		}

		outputBuffer += w.WriteLink(title, item.Link, true, readingTime)

		if rss.summarize {
			if isSimilar && summary == "" {
				similar++
				outputBuffer += w.WriteSummary("↳ Same story as: "+citationTitle(repTitle), true)
			} else {
				outputBuffer += w.WriteSummary(summary, true)
			}
		}

		if cfg.ShowImages && !cfg.TerminalMode {
			img := extractImageTagFromHTML(item.Content)
			if img != "" {
				outputBuffer += img + "\n"
			}
		}

		if !seenToday {
			store.MarkAsSeen(item.Link, summary, item.Title, feed.Title, feed.FeedLink)
		}
	}

	fmt.Printf("Feed %q: %d items read, %d already seen on earlier days, %d already shown above in this digest, %d new (%d similar to another story, not summarized)\n", feed.Title, total, alreadySeen, shownAbove, total-alreadySeen-shownAbove, similar)

	if itemsFound && outputBuffer != "" {
		header := w.WriteHeader(feed)
		w.Write(header + outputBuffer)
	} else if cfg.ShowEmptySections {
		w.Write(w.WriteHeader(feed) + w.WriteSummary(emptyFeedNote(rss.url, total, alreadySeen, shownAbove), true))
	}
}

func getSummary(llm *LLMClient, item *gofeed.Item, cfg *Config) string {
	fmt.Printf("Summarizing: %s\n", item.Link)
	if llm != nil {
		if md := getPaperMarkdown(item.Link, cfg); md != "" {
			return summarizePaperText(llm, md)
		}
		link := unwrapGoogleRedirect(item.Link) // Google Alerts feeds wrap the publisher URL
		if isGoogleNewsLink(link) {
			text, err := googleNewsArticleText(link)
			if err != nil {
				log.Printf("Skipping summary for %s: %v", item.Link, err)
				return ""
			}
			fmt.Printf("  fetched %d characters of article text\n", len(text))
			return llm.Summarize(text)
		}
		content := item.Description
		if text, err := fetchArticleText(link); err == nil {
			content = text
			fmt.Printf("  fetched %d characters of article text\n", len(text))
		} else {
			log.Printf("Could not fetch %s, summarizing feed description instead: %v", link, err)
		}
		return llm.Summarize(content)
	}
	return item.Description
}

// getPaperMarkdown returns the full text of an arXiv paper parsed by MinerU,
// or "" when MinerU isn't configured, the link isn't a paper, or parsing fails.
func getPaperMarkdown(link string, cfg *Config) string {
	mc := NewMinerUClient(cfg.MinerUURL, cfg.MinerUTier)
	pdfURL, ok := arxivPDFURL(link)
	if mc == nil || !ok {
		return ""
	}
	fmt.Printf("Parsing paper with MinerU: %s\n", pdfURL)
	md, err := mc.PDFToMarkdown(pdfURL)
	if err != nil {
		log.Printf("MinerU failed for %s, falling back to abstract: %v", link, err)
		return ""
	}
	return md
}

func getReadingTime(link string) string {
	article, err := readability.FromURL(link, 30*time.Second)
	if err != nil {
		return ""
	}

	words := strings.Fields(article.TextContent)
	if len(words) == 0 {
		return ""
	}

	// 200 wpm
	minutes := len(words) / 200
	if minutes == 0 {
		return "" // < 1 min
	}
	return strconv.Itoa(minutes) + " min"
}

func extractImageTagFromHTML(htmlText string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlText))
	if err != nil {
		return ""
	}

	imgTags := doc.Find("img")
	if imgTags.Length() == 0 {
		return ""
	}

	firstImgTag := imgTags.First()

	// Resize logic
	width := firstImgTag.AttrOr("width", "")
	height := firstImgTag.AttrOr("height", "")

	if width != "" && height != "" {
		wInt, _ := strconv.Atoi(width)
		hInt, _ := strconv.Atoi(height)

		if wInt > 0 && hInt > 0 {
			aspectRatio := float64(wInt) / float64(hInt)
			const maxWidth = 400

			if wInt > maxWidth {
				wInt = maxWidth
				hInt = int(float64(wInt) / aspectRatio)
			}

			firstImgTag.SetAttr("width", fmt.Sprintf("%d", wInt))
			firstImgTag.SetAttr("height", fmt.Sprintf("%d", hInt))
		}
	}

	html, err := goquery.OuterHtml(firstImgTag)
	if err != nil {
		return ""
	}

	return html
}

func formatHackerNewsLinks(w Writer, item *gofeed.Item) string {
	desc := item.Description
	commentsURL := ""

	if start := strings.Index(desc, "Comments URL"); start != -1 {
		safeStart := start + 23
		if safeStart+45 < len(desc) {
			commentsURL = desc[safeStart : safeStart+45]
		}
	}

	// Find count
	count := 0
	if start := strings.Index(desc, "Comments:"); start != -1 {
		s := desc[start+10:]
		s = strings.Replace(s, "</p>\n", "", -1)
		s = strings.TrimSpace(s)
		count, _ = strconv.Atoi(s)
	}

	icon := "💬 "
	if count >= 100 {
		icon = "🔥 "
	}

	// If parsing failed, default to item.Link (often the comments page for text posts)
	if commentsURL == "" {
		commentsURL = item.Link
	}

	return w.WriteLink(icon, commentsURL, false, "")
}

func getInstapaperLink(link string) string {
	return fmt.Sprintf(`[<img height="16" src="https://staticinstapaper.s3.dualstack.us-west-2.amazonaws.com/img/favicon.png">](https://www.instapaper.com/hello2?url=%s)`, link)
}

func stripHtmlRegex(s string) string {
	const regex = `<.*?>`
	r := regexp.MustCompile(regex)
	return r.ReplaceAllString(s, "")
}

func runGenerateAll(cfg *Config, store *Storage) {
	fmt.Println("Regenerating all daily digests from database...")

	allData, err := store.GetAllArticles()
	if err != nil {
		log.Fatalf("Error querying database: %v", err)
	}

	var dates []string
	for d := range allData {
		dates = append(dates, d)
	}
	sort.Strings(dates) // 2023-01-01, 2023-01-02...

	for _, date := range dates {
		items := allData[date]
		fmt.Printf("Processing %s (%d articles)... \n", date, len(items))

		mw := NewMarkdownWriter(cfg, date)
		os.Remove(mw.FilePath)

		// Group items by Feed Title so we can create sections
		feeds := make(map[string][]ArchivedItem)
		var feedOrder []string // To keep consistent order

		for _, item := range items {
			if _, exists := feeds[item.FeedTitle]; !exists {
				feedOrder = append(feedOrder, item.FeedTitle)
			}
			feeds[item.FeedTitle] = append(feeds[item.FeedTitle], item)
		}
		sort.Strings(feedOrder)

		for _, feedTitle := range feedOrder {
			feedItems := feeds[feedTitle]

			// Write Feed Header (using the first item's feed URL for favicon)
			firstItem := feedItems[0]
			mw.Write(mw.WriteFeedHeaderRaw(feedTitle, firstItem.URL))

			for _, item := range feedItems {
				var line string

				// Instapaper Icon (if enabled)
				if cfg.Instapaper {
					instapaperURL := fmt.Sprintf("https://www.instapaper.com/hello2?url=%s", item.URL)
					line += fmt.Sprintf(`[<img height="16" src="https://staticinstapaper.s3.dualstack.us-west-2.amazonaws.com/img/favicon.png">](%s)`, instapaperURL)
				}

				// Article Title Link
				line += fmt.Sprintf("[%s](%s)  \n", item.Title, item.URL)
				mw.Write(line)

				// Summary (if exists)
				if item.Summary != "" {
					mw.Write(mw.WriteSummary(item.Summary, true))
				}
			}
		}
	}
	fmt.Println("Done.")
}
