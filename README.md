<img align="right" src="https://github.com/piqoni/matcha/actions/workflows/test.yml/badge.svg">
<h1 align="center"> 🍵 Matcha </h1>
<div align="center"><p>
    <a href="https://github.com/piqoni/matcha/releases/latest">
      <img alt="Latest release" src="https://img.shields.io/github/v/release/piqoni/matcha?style=for-the-badge&logo=starship&color=C9CBFF&logoColor=D9E0EE&labelColor=302D41" />
    </a>
    <a href="https://github.com/piqoni/matcha/pulse">
      <img alt="Last commit" src="https://img.shields.io/github/last-commit/piqoni/matcha?style=for-the-badge&logo=starship&color=8bd5ca&logoColor=D9E0EE&labelColor=302D41"/>
    </a>
    <a href="https://github.com/piqoni/matcha/blob/main/LICENSE">
      <img alt="License" src="https://img.shields.io/github/license/piqoni/matcha?style=for-the-badge&logo=starship&color=ee999f&logoColor=D9E0EE&labelColor=302D41" />
    </a>
    <a href="https://github.com/piqoni/matcha/stargazers">
      <img alt="Stars" src="https://img.shields.io/github/stars/piqoni/matcha?style=for-the-badge&logo=starship&color=c69ff5&logoColor=D9E0EE&labelColor=302D41" />
    </a>
</div>

Matcha is a daily digest generator for your RSS feeds and interested topics/keywords. This fork is tuned for **AI CAD**: text/image-to-CAD, CadQuery and OpenSCAD code generation, parametric modeling, and LLM agents that drive CAD tools. The default config follows those sources and keywords, and the Analyst screens new papers for relevance to an AI design-studio tool. By using any markdown file viewer (such as [Obsidian](https://obsidian.md/)) or directly from terminal (-t option), you can read your RSS articles whenever you want at your pace, thus avoiding FOMO throughout the day.

### In Obsidian
<img width="900" alt="image" src="https://user-images.githubusercontent.com/3144671/219786799-55db70c1-5860-4d4b-9df4-b81a89f8161d.png">

### On the terminal

<img width="596" alt="image" src="https://user-images.githubusercontent.com/3144671/208323296-af2d6a51-7d33-42a9-a827-0e96a4a383fd.png">

### On the Web
[Read more here how to setup the web version](https://github.com/piqoni/go-digest)

<img width="1080" height="676" alt="image" src="https://github.com/user-attachments/assets/6bc348a2-803d-4ed4-9bd5-e896d850b672" />

## Features
 - RSS daily **digest**, it will show only articles not previously generated
 - Optional summary of articles from OpenAI/Local LLM models for selected feeds
 - Weather for the next 12 Hours (from [YR](https://www.yr.no/))
 - Quick bookmarking of articles to Instapaper
 - Interested Topics/Keywords to follow (through [Google News](https://news.google.com/))
 - Hacker News comments direct link and distinguishing mostly dicussed posts 🔥
 - Terminal Mode by calling `./matcha -t`
 - LLM Notifications: get notified when your custom prompt is fulfilled (example: "Notify me when X company is sold")
 - LLM Analyst: Performs an analysis of article titles and responds based on your prompt (example: "Tell me if you see any investment opportunity")


## Installation & Usage
1. Since Matcha generates markdown, any markdown reader should do the job. Currently it has been tested on [Obsidian](https://obsidian.md/) so you need a markdown reader before moving on, unless you will use terminal mode (-t option), then a markdown reader is not needed.
2. **Download the [pre-built Matcha binary](https://github.com/piqoni/matcha/releases)** based on your OS and after executing (if on mac/linux run `chmod +x matcha-darwin-amd64` to make it executable), a sample `config.yml` will be generated, which you can add in your rss feeds, keywords and the `markdown_dir_path` where you want the markdown files to be generated (if left empty, it will generate the daily digest on current dir).
3. You can either execute matcha on-demand (a terminal alias) or set a cron to run matcha as often as you want. Even if you set it to execute every hour, matcha will still generate daily digests, one file per day, and will add more articles to it if new articles are published throughout the day.
### Running daily on a Mac that sleeps
A Mac that is asleep does not run `launchd` or `cron` jobs, and plugged in with the lid closed it sleeps too (Clamshell Sleep). It only wakes briefly for DarkWake maintenance windows of about 3 minutes, which is not long enough for a digest with paper summaries. Two pieces fix this: a scheduled wake a few minutes before the run, and running Matcha under `caffeinate -i -s` so the Mac stays awake until it finishes (`-s` only holds while on AC power).

`scripts/macos/install-schedule.sh` sets both up:
```
go build -o matcha .
scripts/macos/install-schedule.sh --time 00:05      # run at 00:05, wake the Mac at 00:02
```
- It writes `~/Library/LaunchAgents/io.matcha.daily.plist` and loads it. The job starts at `--time` and again every 15 minutes as a catch-up, and runs `scripts/macos/run-daily.sh`, which runs `./matcha` at most once a day, only on AC power and only once the network answers (it waits up to a minute after a wake). The last successful run date is kept in `~/Library/Application Support/Matcha/last-run`; output goes to `~/Library/Logs/matcha-daily.log`.
- After a successful run, `run-daily.sh` calls `scripts/macos/qmd-refresh.sh`, which runs `qmd update` and then `qmd embed` (under `caffeinate`) so your digests are searchable in qmd. It is skipped if `qmd` is not found, and a qmd failure is only logged; it never undoes the day's stamp. launchd has a minimal `PATH`, so if `qmd` is not found there (the fallback is `~/.bun/bin/qmd`), set `QMD_BIN` to its full path in the plist's `EnvironmentVariables`. Your `markdown_dir_path` must be in a qmd collection.
- It then runs `sudo pmset repeat wakeorpoweron MTWRFSU <time minus --wake-lead>` (default lead 3 minutes), so **it asks for your password**. It prints `pmset -g sched` before and after. macOS keeps a single repeating wake/power-on event, so this replaces any you already had.
- Options: `--wake-lead N` minutes (1 to 60), `--dir PATH` (folder with the `matcha` binary and `config.yaml`, default the repo), `--no-wake` (skip the `pmset` step and sudo), `--dry-run` (print the plist and commands, change nothing).

If you already start Matcha from your own LaunchAgent or script, you do not need the installer: wrap the job's `ProgramArguments` in `/usr/bin/caffeinate -i -s ...`, give it a `StartCalendarInterval` at the run time (keep any `StartInterval` catch-up), and run `sudo pmset repeat wakeorpoweron MTWRFSU HH:MM:SS` a few minutes earlier. Remove an old LaunchAgent that also runs Matcha so it does not run twice.

To undo:
```
scripts/macos/uninstall-schedule.sh               # unloads and deletes the LaunchAgent, then: sudo pmset repeat cancel
scripts/macos/uninstall-schedule.sh --keep-wake   # leave the pmset wake in place
pmset -g sched                                    # check what is still scheduled
```
`sudo pmset repeat cancel` removes every repeating power event, not just Matcha's. One-off wakes from `pmset schedule` are listed separately and are cancelled with `sudo pmset schedule cancel <n>`.

### Other ways to install
#### Homebrew (macOS)
```
brew install matcha
```
#### Go devs
```
go install github.com/piqoni/matcha@latest
```
## Configuration
On first execution, Matcha will generate the following config.yaml and a markdown file on the same directory as the application. Change the 'feeds' to your actual RSS feeds, and google_news_keywords to the keywords you are interested in. And if you want to change where the markdown files are generated, set the full directory path in `markdown_dir_path`.

```yaml
markdown_dir_path:
summary_feeds:
arxiv_searches:
  - name: CAD tools and generation   # precise; every hit is parsed in full
    use: summary                     # summary | analyst | feed
    limit: 5
    terms:
      - CadQuery
      - Build123d
      - text-to-CAD
      - image-to-CAD
      - sketch-to-CAD
      - CAD generation
      - CAD code
      - CAD modeling
      - CAD editing
      - parametric CAD
      - B-rep
      - OpenSCAD
  - name: Broad CAD scan             # noisy; an analyst screens it
    use: analyst
    analyst: CAD research scout      # which analyst reads it (default: the first)
    limit: 30
    terms:
      - CadQuery
      - Build123d
      - CAD
      - computer-aided design
      - parametric modeling
      - solid modeling
feeds:
  - https://hnrss.org/newest?q=CadQuery+OR+Build123d+OR+OpenSCAD+OR+%22text-to-CAD%22+OR+%22AI+CAD%22 10
  - https://hackaday.com/category/3d-printer-hacks/feed/ 10
  - https://www.reddit.com/r/OpenSCAD/.rss 10
  - https://www.reddit.com/r/cad/.rss 10
google_news_keywords: CadQuery,Build123d,text-to-CAD,AI CAD,generative CAD,parametric CAD,OpenSCAD
summarize_google_news: true
cluster_similar_stories: true
show_empty_sections: true
instapaper: true
weather_latitude: 37.77
weather_longitude: 122.41
terminal_mode: false
opml_file_path:
markdown_file_prefix:
markdown_file_suffix:
reading_time: false
sunrise_sunset: false
openai_api_key:
openai_base_url:
openai_model:
summary_prompt:
paper_summary_prompt:
mineru_url: http://localhost:8000
mineru_tier:
paper_store_dir:
paper_store: picks
paper_store_zip: false
show_images: false
analysts:                            # each analyst screens its feeds with its own prompt and gets its own section
  - name: CAD research scout
    model:                             # optional; defaults to openai_model
    read_papers: true                  # pick arXiv papers for full-text summaries (MinerU)
    max_papers: 3
    feeds:                             # "URL N"; arxiv_searches with use: analyst are added here too
    prompt: >-
      You are a strict research scout for an AI-assisted CAD product that generates and edits parametric
      CAD as code (CadQuery, Build123d, OpenSCAD). Most days only a few of the numbered articles qualify,
      often none. Pick an article only if its title or abstract is directly about generating, editing,
      verifying or repairing 3D CAD models with AI (text/image/sketch-to-CAD, CAD code generation, B-rep
      or CAD-ready mesh generation), design intent or geometric constraints in CAD, LLM agents that drive
      CAD kernels or tools, CAD datasets and benchmarks, or AI for design for manufacturing. Do not pick
      an article by analogy, such as general code generation, theorem proving, robotics, simulation, image
      or video generation, 3D scene reconstruction, or optimization of unrelated systems, unless it
      explicitly applies the work to CAD or mechanical part design. Ignore unrelated uses of the acronym
      CAD (computer-aided diagnosis or detection). Pick at most 8. For each pick, give a one-line takeaway
      and why it matters for a design-studio tool.
  - name: Creator manufacturing demand
    max_chars: 600                     # characters of each post the model sees (default 1500)
    feeds:
      - https://www.reddit.com/r/Entrepreneur+smallbusiness+ecommerce+shopify+startups+EtsySellers+FulfillmentByAmazon/search.rss?q=manufacturer+OR+manufacturing+OR+prototype+OR+MOQ&restrict_sr=on&sort=new 25
      - https://www.reddit.com/r/PartneredYoutube+NewTubers+youtubers+InstagramMarketing+Twitch+podcasting/search.rss?q=merch+OR+%22product+line%22+OR+%22own+product%22+OR+manufacturer&restrict_sr=on&sort=new 15
      - https://www.reddit.com/r/IndustrialDesign+productdesign+manufacturing+AskEngineers+3Dprinting/search.rss?q=%22contract+manufacturer%22+OR+%22injection+molding%22+OR+%22small+batch%22+OR+%22production+run%22&restrict_sr=on&sort=new 15
      - https://hnrss.org/newest?q=%22contract+manufacturer%22 10
      - https://hnrss.org/newest?q=%22injection+molding%22 10
      - https://www.modernretail.co/feed/ 15
      - https://www.tubefilter.com/feed/ 15
    google_news:                       # plain Google News searches, first 10 results each
      - '"creator brand" OR "creator-led brand" OR "influencer brand" OR "creator-owned brand" when:30d'
      - 'creator OR influencer OR YouTuber "product line" when:30d'
      - '"minimum order quantity" OR MOQ small brand manufacturer when:7d'
      - 'Kickstarter OR Indiegogo delayed manufacturing when:30d'
      - 'Pietra OR Sourcify OR "Maker''s Row" when:30d'
    prompt: >-
      You are a market-signal scout for a product-design tool that takes content creators and
      independent brands from a product idea to a manufacturable design and quotes from contract
      manufacturers, for runs of about 1,000 to 10,000 units. The numbered items are forum posts and
      news. Most days only a few qualify, often none. Pick an item only if it is about a physical
      product (not apps, courses or other digital products) and shows one of these:
      a creator, small brand or first-time founder describing trouble making a physical product
      (finding or vetting a manufacturer, minimum order quantities, quotes, prototype cost or delays,
      needing CAD or an industrial designer, tooling cost, quality problems, small-run economics);
      a creator or independent brand wanting to launch their own physical product or product line
      beyond print-on-demand merch; a creator-led or small brand launching, funding or delaying a
      manufactured product, or a company offering design, sourcing or small-batch manufacturing to
      them; or evidence against the idea, such as creators saying existing services already solve
      this or that small runs are easy and cheap.
      Do not pick general business or marketing advice, dropshipping or reselling existing products,
      print-on-demand shirts and mugs with no custom product, software, or large companies' product news.
      Pick at most 8. For a post, quote a few of the poster's own words.
```

### `summary_feeds` vs `analysts`
Both use an LLM, but they do different jobs. (The config key is `summary_feeds`, not `summarize_feeds`.)

| | `summary_feeds` | `analysts` |
|---|---|---|
| **Purpose** | Read each article for you | Screen a batch of articles and tell you which matter |
| **Appears in the digest as** | The feed's articles, each with its own LLM summary | One section per analyst, named after it; the feeds' articles are not listed separately |
| **LLM input** | One article per call: the full page (or, for arXiv, the full paper via MinerU) | One call per analyst for the whole batch: titles and descriptions only |
| **Prompt** | `summary_prompt` / `paper_summary_prompt` | each analyst's `prompt` (you write what to look for) |
| **Model** | `openai_model` | each analyst's `model` (defaults to `openai_model`, then gpt-4o) |
| **Cost** | Up to one LLM call per article (keep limits small) | One call per analyst per run, plus full-paper summaries for up to `max_papers` picks |
| **Best for** | Feeds you want condensed, such as a few high-signal sources | Noisy or high-volume feeds you want triaged, or alerts (`notification_trigger`) |

Both accept the same `URL N` limit syntax. The two can share a source: the default config summarizes a narrow arXiv query and has the CAD research scout screen a broader one, then promote its picks to full-paper summaries (`read_papers`). Plain `feeds` are neither: they are listed with headlines only.

### arXiv searches
Instead of hand-writing arXiv's URL-encoded `search_query`, list plain terms under `arxiv_searches`. Matcha builds the query-API feed for you:

```yaml
arxiv_searches:
  - name: Robotics CAD            # label for warnings only
    use: analyst                  # summary | analyst | feed
    limit: 30                     # newest N results kept
    field: all                    # optional: all (default) | ti | abs
    terms:                        # match ANY term; multi-word and hyphenated terms become phrases
      - CadQuery
      - text-to-CAD
      - CAD generation
    categories: [cs.RO, cs.AI]    # optional: also require one of these categories (cs.* works)
    exclude:                      # optional: drop results matching any of these
      - computer-aided diagnosis
    analyst: CAD research scout   # use: analyst only: which analyst reads it (default: the first)
```

- `use: summary` adds it like `summary_feeds` (each hit is parsed in full with MinerU and summarized; default limit 5). `use: analyst` adds it to an analyst's feeds (cheap screening of titles and abstracts; default 30): the one named by `analyst:`, or else the first analyst. `use: feed` lists links only (default 20).
- Results are always newest-first. Bad entries (unknown `use`/`field`, no terms, malformed category) are skipped with a warning in the output.
- Raw feed URLs still work in `feeds`, `summary_feeds` and an analyst's `feeds`; `arxiv_searches` entries are added to them.

### Analysts
An analyst gathers the newest items from its feeds, sends their titles and descriptions to a language model in one prompt, and writes the model's picks into the digest under a section with the analyst's name. You write in its `prompt` what it should look for: papers relevant to your product, signs of a market, an investment opportunity, a specific kind of security incident. Each analyst has its own feeds and prompt, so several topics don't dilute each other. The default config ships two: a CAD research scout and a creator manufacturing demand scan.

```yaml
analysts:
  - name: CAD research scout          # section title; also keys what this analyst has already screened
    model:                            # optional; defaults to openai_model, then gpt-4o
    read_papers: true                 # optional: pick arXiv papers for full-text summaries (see below)
    max_papers: 3                     # optional: at most this many picks (default 3)
    max_chars: 1500                   # optional: characters of each item's text sent to the model (default 1500)
    feeds:                            # "URL N", default 20 items; arxiv_searches with use: analyst are added here
      - https://rss.arxiv.org/rss/cs.CV 30
    google_news:                      # optional plain Google News searches; first 10 results each
      - '"text-to-CAD" when:7d'
    prompt: >-
      What to pick. The numbered output format is added for you.
```

- Analysts run in the order listed, before the feeds. Each costs one LLM call per run.
- Every item is screened once per analyst: an article one analyst already saw is still shown to the others. Renaming an analyst makes it screen its feeds again.
- The model sees each item's title and description, or the item's content when there is no description (Reddit's feeds put the post itself only there), without HTML and cut to `max_chars`. Forum posts can be long, and a small local model's context fills quickly: with Ollama, check `num_ctx` against roughly (items x `max_chars` / 4) tokens.
- The Analyst reads only the first N items of each feed and does no filtering itself, so the feed or search query, not the model, decides what it can see.
- `./matcha -check-feeds` lists each analyst's feeds under its name.
- Entries without a name, a prompt, or any feeds are skipped with a warning in the log, as is a second analyst with the same name.

Another example, an analyst finding investment opportunities:

```yaml
openai_api_key: sk-xxxxxxxxxxxxxxxxx
analysts:
  - name: Investment ideas
    feeds:
      - https://feeds.bbci.co.uk/news/business/rss.xml
    prompt: You are a world-class investing expert. Analyze the provided list of articles for potential investment opportunities. If no direct opportunities are found, identify industries, regions, or trends that could have indirect impacts on the investment landscape.
```

Snippet of sample output (as an investment analyst):
<img width="961" alt="image" src="https://github.com/user-attachments/assets/5ccb43d0-3057-4b39-b445-891246c9b644" />

#### Older configs: `analyst_feeds` and `analyst_prompt`
Before `analysts`, Matcha had one Analyst configured with top-level keys. They still work: `analyst_feeds`, `analyst_prompt`, `analyst_model`, `analyst_read_papers` and `analyst_max_papers` become an analyst named "Daily Analysis" that runs first, keeps its screening history, and receives `arxiv_searches` that name no analyst. `analyst_model` is also the default model for the other analysts. To switch, move them into an `analysts` entry (`analyst_feeds` → `feeds`, `analyst_prompt` → `prompt`, `analyst_read_papers` → `read_papers`, `analyst_max_papers` → `max_papers`) and delete the old keys. The renamed analyst screens the items still in its feeds once more on its first run, because its screening history is kept under its new name.

#### The default: creator manufacturing demand
The second default analyst looks for evidence that content creators and independent brands want to manufacture their own physical products and find the process hard. It reads:
- **Reddit searches** (newest first) across small-business and e-commerce subreddits, creator subreddits (YouTube, Instagram, Twitch, podcasting), and design and manufacturing subreddits, for words such as manufacturer, prototype, MOQ, contract manufacturer and injection molding. Reddit's search syntax takes `OR` and quoted phrases; edit `q=` in the URL to change the terms or the `r/a+b+c` part to change the subreddits.
- **Hacker News** posts mentioning contract manufacturers or injection molding (hnrss.org).
- **Trade press**: Modern Retail (DTC and creator-led brands) and Tubefilter (creator economy).
- **Google News** searches for creator brands and manufacturing, creators launching product lines, MOQ complaints, crowdfunded products delayed by manufacturing, and companies that already serve this market (Pietra, Sourcify, Maker's Row). `when:7d` or `when:30d` limits a search to the last week or month; the narrower searches use 30 days because a week often returned nothing.

The prompt picks first-hand accounts of trouble getting a product made (finding a manufacturer, MOQs, quotes, prototype cost, needing CAD or an industrial designer, small-run economics), creators who want to launch a physical product, market news (launches, funding, delays, competitors), and evidence against the idea, such as "existing services already solve this". It quotes posters. A pick reads like:

```
- **[Factory wants a 5,000 unit MOQ for my first product](https://www.reddit.com/...)** Poster says "quotes are all over the place". Why it matters: ...
```

These feed URLs were written without being able to fetch them from the development sandbox, so run `./matcha -check-feeds` once and drop any that fail. Reddit sometimes answers RSS requests with `429` or `403` when it sees too much traffic from one address.

#### Analyst output format
When the articles are numbered (the default, except for the first analyst when `notification_trigger` is set), Matcha appends a strict format to the analyst's `prompt`: one bullet per relevant article, written as `- [n] <takeaway>. Why it matters: <reason>.`, or `No relevant articles today.` Matcha then replaces each `[n]` with the article's real title as a bold link, so the analysis names every paper and links to it without trusting the model to copy titles or URLs. Keep your own prompt about *what* to pick; the format is handled for you.

Some local models running in thinking mode (Gemma's thinking channel, Qwen or DeepSeek `<think>` blocks) can return their reasoning as part of the answer, which used to show up in the digest as a draft list followed by the real one. Matcha keeps only the text after the last `<channel|>` or `</think>` marker, for the Analyst and for summaries.

#### Feed limits and arXiv category feeds
The limit cuts a feed *before* the model sees it. That matters for arXiv's category RSS feeds (`https://rss.arxiv.org/rss/cs.CV`): each lists the whole latest announcement, often 300 to 500 papers, so `cs.CV 30` screens only the first 30 and the rest of the day's papers are never shown to the Analyst. Run `./matcha -check-feeds` to see how many items each analyst feed returns. Your options:
- **Keep the broad sweep and raise the limit** (`https://rss.arxiv.org/rss/cs.CV 500`). The Analyst then sees every paper in the category, at the cost of a much longer prompt: roughly 300 tokens per paper (title plus abstract), so 500 papers is about 150k tokens. Check that your model's context window holds it (with Ollama, `num_ctx`), or the input is silently cut.
- **Narrow with a search** (`arxiv_searches` with `use: analyst`). The query picks papers that mention your terms in any category, and the Analyst screens those. Fewer papers, all of them plausible.
- **Both:** one broad category feed with a high limit plus a search, if your model and hardware can take the prompt.

The category RSS feeds are also empty between announcements (weekends, holidays, and sometimes for a while after arXiv's daily update). An empty analyst feed is reported in the analyst's section as `N of M feeds returned no items`; run Matcha again later to pick up the day's papers. Items first screened today are screened again on a same-day rerun.

#### Writing an analyst prompt
With a broad input, the model's habit is to find a reason why almost everything matters ("it validates the concept of…"). A prompt that keeps the picks useful:
- **Say how rare a pick is:** "most days only a few articles qualify, often none."
- **Name the topics positively and the near misses explicitly.** Papers on code generation, robotics, simulation, image editing or 3D reconstruction are near misses for a CAD tool; tell the model not to pick them by analogy unless they apply the work to CAD.
- **Cap the number of picks** ("pick at most 8"), so a noisy day can't produce a list of 25.
- **Ask about the title and abstract**, not about what the work could be used for.

The default CAD research scout's prompt follows these rules. Small local models (such as a 4B model) are noticeably more permissive than large ones; a larger `model` is the other lever if the picks are still loose.

#### Analyst picks papers for full-text summaries
With `read_papers: true`, an analyst ends its answer with a `SELECTED:` line naming up to `max_papers` (default 3) articles worth reading in full. Matcha then parses each selected arXiv paper with MinerU (see "Paper summaries with MinerU"), summarizes it, and lists it under "Papers worth reading in full" right after the analysis. Selections are resolved by number against the articles actually sent, so the model can't pick a paper it wasn't shown. Non-arXiv picks stay in the analysis text only.

#### Analyst Notifications
The first analyst can send notifications (Slack hooks or ntfy.sh) when its whole reply equals `notification_trigger`. See a working example below:
```yaml
analysts:
  - name: Flights
    model: qwen3:4b
    feeds:
      - https://feeds.bbci.co.uk/news/business/rss.xml
    prompt: "Check the news titles below and if you see that airbus flights are returned to normal please respond with \"FLIGHTS BACK TO NORMAL\" and nothing else, no explanations."
openai_base_url: http://localhost:11434/v1
notification_trigger: "FLIGHTS BACK TO NORMAL"
notification _webhook_url: https://ntfy.sh/myuniquetopic
```
### Summaries for Google News stories
With `summarize_google_news: true` (the default) and an LLM configured, each story from `google_news_keywords` is summarized like a `summary_feeds` item, instead of showing only the headline. Google News links are encrypted tokens that no longer redirect, so Matcha asks Google's own web endpoint (the one news.google.com calls) for the publisher URL, then fetches and summarizes that page. That endpoint is undocumented and may change or rate-limit; when decoding fails, or the publisher blocks the request or is paywalled, the story keeps just its headline and Matcha logs `Skipping summary for …` with the reason. Set the option to `false` to go back to headlines only. The limit is 15 stories per run (up to 15 LLM calls).

Google Alerts feeds (`https://www.google.com/alerts/feeds/...`) are more dependable: their links carry the publisher URL directly, so Matcha uses it without any decoding. Put them in `summary_feeds` to have them summarized.

### Google News keywords and exclusions
`google_news_keywords` is a comma-separated list; the terms are OR-ed together. Terms starting with `-` are exclusions that apply to the whole search, not extra OR branches:

```yaml
google_news_keywords: '"text-to-CAD", CadQuery, "AI CAD", -forex -loonie -USD/CAD'
# searches: ("text-to-CAD" | CadQuery | "AI CAD") -forex -loonie -USD/CAD
```

### Saving parsed papers
MinerU's job results are temporary: Matcha downloads the markdown, deletes MinerU's copy, and by default keeps nothing, so a paper you want to re-read, re-summarize or search later would have to be parsed again (minutes of GPU time each). Set a folder to keep them:

```yaml
paper_store_dir: ~/papers     # empty = save nothing (the default)
paper_store: picks            # picks (default): only papers an analyst selected; all: every paper Matcha parses
paper_store_zip: false        # also keep MinerU's zip package (markdown, structured JSON, extracted figures)
```

Each paper is saved under its arXiv ID:

```
~/papers/2610.13309v1.md    # front matter (title, source, pdf, date, parser) + MinerU's markdown
~/papers/2610.13309v1.zip   # only with paper_store_zip: true
```

- `picks` keeps the folder small: each analyst with `read_papers` picks at most `max_papers` papers a day. `all` also keeps every paper parsed from `summary_feeds` and `arxiv_searches` with `use: summary`. Markdown for a paper is typically tens to a few hundred kilobytes; that is an estimate, not a measurement.
- A paper that is already in the folder is read from disk instead of being sent to MinerU, in either mode, so the folder also survives deleting `matcha.db`. A revised arXiv version (`v2`) is a different file.
- The zip is kept only when `paper_store_zip` is on and the paper is being saved; if MinerU doesn't return one, the markdown is still saved. With MinerU 4.1+ it is the task's result ZIP (`markdown.md`, `middle_json.json`, `structured_content.json`, `images/`), which Matcha downloads anyway to get the markdown. The legacy `/file_parse` API has no zip.
- The folder is plain files, so other tools can use it: add it to a [Msty](https://msty.app) Knowledge Stack, open it as an Obsidian vault, `grep -r` it, or import files into Zotero by hand. Papers you add to Zotero are parsed again by the llm-for-zotero plugin into its own cache; Matcha does not write to Zotero or that cache.
- Papers parsed before you turned this on are not backfilled, and summaries already cached in the database are not regenerated.

### Resetting what Matcha remembers
Matcha remembers what it has shown in a small SQLite database, `matcha.db`: every article link with its date and cached summary, plus which Analyst notifications were already sent. Anything shown on an earlier day is skipped on later runs; items shown *today* are listed again (with their cached summaries). Deleting rows is how you re-run a day from scratch.

**Where it is**

| OS | Default location |
|---|---|
| macOS | `~/Library/Application Support/brew/matcha.db` |
| Linux | `~/.config/brew/matcha.db` |
| Windows | `%AppData%\brew\matcha.db` |

If `database_file_path` is set in `config.yaml`, use that file instead. The commands below need the `sqlite3` command-line tool (included with macOS and most Linux distributions). Quit Matcha first, and copy the file if you might want it back (`cp matcha.db matcha.db.bak`).

In the commands, `DB` stands for the path above, for example:

```
DB="$HOME/Library/Application Support/brew/matcha.db"
```

**Delete only today's data.** Today's items are processed again on the next run, with fresh summaries; earlier days stay skipped.

```
sqlite3 "$DB" "DELETE FROM seen WHERE date = date('now','localtime'); DELETE FROM notifications WHERE date = date('now','localtime');"
```

**Delete only the most recent day Matcha has data for.** This is the same as above when Matcha already ran today, and otherwise removes the last day it did run (for example yesterday). The order matters: the `notifications` rows are removed first, because the second statement empties the table that supplies "the most recent day".

```
sqlite3 "$DB" "DELETE FROM notifications WHERE date = (SELECT MAX(date) FROM seen); DELETE FROM seen WHERE date = (SELECT MAX(date) FROM seen);"
```

**Delete everything** (every item is new again and every summary is regenerated, which means a full LLM and MinerU run):

```
rm "$DB"
```

Matcha creates a fresh, empty database on the next run. To clear only the memory but keep the table structure, `sqlite3 "$DB" "DELETE FROM seen; DELETE FROM notifications;"` does the same thing.

**To see what is there first**

```
sqlite3 "$DB" "SELECT date, COUNT(*) FROM seen GROUP BY date ORDER BY date DESC LIMIT 7;"
```

Things to know:
- Dates are stored in your local time (`YYYY-MM-DD`), so the commands use `date('now','localtime')`. SQLite's plain `date('now')` is UTC and can be a day off in the evening.
- The analysts' own bookkeeping is stored in the same table (their links end in `#analyst` or `#analyst:<name>`), so deleting a day also makes the analysts screen that day's articles again.
- Deleted days vanish from `./matcha -generate-all`, which rebuilds the daily files from this database.
- A digest file for a day you delete stays on disk until the next run rewrites it; Matcha rewrites today's file at the start of every run.
- Saved papers (`paper_store_dir`) are separate. Papers already there are reused rather than re-parsed even after a reset, so re-running a day costs LLM time but not MinerU time. To force a re-parse, delete the `.md` (and `.zip`) files for those papers.
- Matcha does not create lock files; there is nothing else to remove.

### Code and data links
Summaries of papers, news stories and blog posts end with a `**Code/data:**` line when the source mentions a repository or dataset, so you don't have to open the article to find it. For papers this is taken from the MinerU text (references and appendices are trimmed first, so other papers' links don't leak in); for news and Alerts it comes from the article's visible text and from the targets of its links, which the plain text would otherwise lose ("the project's GitHub page"). It is a pattern match, not an LLM step, so it costs nothing extra.

Recognized: GitHub, GitLab, Bitbucket, Codeberg, SourceForge, Hugging Face, PyPI, `*.github.io` project pages, Zenodo, Figshare, OSF and Kaggle. Only project pages count (`github.com/owner/repo`, not `github.com` or `github.com/features/...`), each project is listed once even if linked several times, and at most 5 are shown. Links that PDF extraction splits across lines can be missed.

### Feed fetching, rate limits and `-check-feeds`
Matcha identifies itself (`Matcha/1.0 (+https://github.com/...)`), spaces out requests to arXiv hosts by at least 3 seconds (arXiv's API asks for one request every 3 seconds), and retries a feed that answers `429 Too Many Requests` or `503`: it honors a `Retry-After` header (up to 2 minutes), otherwise waits 5, 20 and 60 seconds, then gives up with `⚠️ Could not read this feed: http error: 429 Too Many Requests`.

To test your feeds without producing a digest, run `./matcha -check-feeds`. It fetches every configured feed (and analyst feed) once with the same pacing and retries, and prints one line each:

```
ok    200  1 attempt(s)    1.2s  arXiv: CAD tools and generation  (5 items)
FAIL  429  4 attempt(s)   88.0s  analyst: https://export.arxiv.org/api/query?...  (http error: 429 Too Many Requests)
```

It uses no LLM, writes no files and does not touch the database, so it is safe to run as often as you like (but each run is real traffic to those sites, so a loop of runs can itself trigger a 429).

### Empty and failed sections
With `show_empty_sections: true` (the default), a section that has nothing to show still gets its header and a one-line note saying why, so an empty section is distinguishable from one that never ran:
- `No items returned by this feed.` (for `rss.arxiv.org` feeds this adds that arXiv's RSS is empty on days without announcements, such as weekends and holidays; the arXiv *search* feeds from `arxiv_searches` use the query API and are not weekend-limited)
- `No new items: 5 read, 5 already seen on earlier days.` (also counts items already shown above in the same digest)
- `⚠️ Could not read this feed: <error>`
- For an analyst's section: `No new articles to screen: …` (with a warning if some analyst feeds could not be read or returned no items), or a note if the model returned no analysis. When there is an analysis but some analyst feeds were empty, a line under it says `N of M analyst feeds returned no items`.

A section that does have new items ends with a short `Not shown again: N already seen on earlier days` line when older items were left out, so you can tell what a manual look at the same feed would additionally show.

`arxiv_searches` sections are titled `arXiv: <name>` using the `name` you gave them, instead of the raw query. Set `show_empty_sections: false` to hide empty sections.

### One summary per story
With `cluster_similar_stories: true` (the default), feeds that are summarized (`summary_feeds`, Google Alerts, Google News) summarize only the first story of a group of near-duplicate headlines, such as five outlets covering the same announcement. The others are still listed with their links, marked `↳ Same story as: <first headline>`, and cost no fetch or LLM call.

Matching is deliberately strict so related-but-different stories stay separate: the outlet suffix (`- Reuters`) and common words are dropped, words that appear in half or more of that feed's headlines (for example "CAD" in a CAD alert, or a person's name in a person-specific alert) are ignored, and two headlines count as the same story only if they share at least three of the remaining words and those words make up at least 40% of their combined remaining words. Clustering only compares items within one feed. Set `cluster_similar_stories: false` to turn it off.

### Paper summaries with MinerU
arXiv links in `summary_feeds` (and papers picked by an analyst) are summarized from the **full paper**, not the abstract. Matcha sends the PDF to a [MinerU](https://github.com/opendatalab/MinerU) API server and feeds the resulting markdown to the LLM with a CAD-oriented prompt.

```yaml
mineru_url: http://localhost:8000   # leave empty to disable
mineru_tier:                        # optional: flash, basic, standard or advanced
paper_summary_prompt:               # optional override
openai_model: gpt-4o                # papers are long; use a large-context model
```

Matcha supports three MinerU API generations and picks one by probing `GET /v1/health`:
- **MinerU 4.1 and later, Tasks API** (`/v1/tasks`), used when the health reply advertises `features.task_output_formats` or a 4.1+ version. For arXiv papers Matcha downloads the PDF itself (following redirects, identifying itself, and pacing requests to arXiv) and uploads it, because MinerU cannot fetch arXiv's PDF URLs on its own: they have no `.pdf` extension (MinerU rejects the file name as an unsupported type) and arXiv redirects the `.pdf` form, which MinerU's downloader does not follow. Other URLs that end in `.pdf` are sent as a URL task first, with the same upload as a fallback. Matcha creates a one-file task (with the PDF's size and SHA-256, so the server can reuse a cached upload), PUTs the PDF to the upload link the server returns, polls the task (up to 15 minutes, then cancels it), downloads the result ZIP and reads `markdown.md` from it. MinerU expires results itself; 4.1 has no delete call.
- **MinerU 3.x and 4.0 job API** (`/v1/uploads`, `/v1/parse/jobs`, removed in 4.1): same download-and-upload rule for arXiv; Matcha creates a markdown job, polls it, downloads the markdown, and deletes the output file. Only one job runs at a time on a default server; a `429` is retried.
- **Older `mineru-api`** (`POST /file_parse`): used when `/v1/health` isn't available.

If `/v1/health` answers `503` (MinerU is up but still loading its models), Matcha logs that and uses the abstract for that paper.

Before summarizing, Matcha drops the references, acknowledgments and appendices (everything from a `References`/`Appendix`-style heading in the second half of the paper) so the character budget goes to the body and results. It also collects code and data links from the body and lists them under the summary as `Code/data:` (see below).

If MinerU is unreachable or fails, Matcha logs it and falls back to the abstract. Parsing can take minutes per paper on CPU, so keep arXiv limits small; summaries are cached in the database so each paper is parsed once. Non-arXiv links are unaffected.

### Summarization of Articles using ChatGPT

In order to use the summarization feature, you'll first need to set up an OpenAI account. If you haven't already done so, you can sign up [here](https://platform.openai.com/login?launch). Once registered, you'll need to acquire an OpenAI API key which can be found [here](https://platform.openai.com/account/api-keys).

Alternatively, you may use LocalAI (see the "LocalAI Support" section below for more information).

Next, update the configuration file with the desired feeds you want to summarize. This can be done under the "summary_feeds" section. Here is an example configuration:

```yaml
openai_api_key: sk-xxxxxxxxxxxxxxxxx
summary_feeds:
    - http://hnrss.org/best 10
```

Replace `sk-xxxxxxxxxxxxxxxxx` with your OpenAI API key and include the RSS feeds under `summary_feeds` for articles you're interested in summarizing.

You can also customize which model you use for summarization by changing the openai_model to one of the values [here](https://github.com/sashabaranov/go-openai/blob/a14bc103f4bc2b3ac40c844079fdf59dfdf62b0b/completion.go#L30) which defaults to 'gpt-3.5-turbo' for now. 'gpt-4' is also a valid model name.

```yaml
openai_model: gpt-3.5-turbo
```

#### LocalAI Support

For those interested in using LocalAI for summarization, whether for cost-efficiency or privacy reasons, you'll first need to set it up and run it. For setup instructions, please visit the LocalAI repository on GitHub [here](https://github.com/go-skynet/LocalAI).

After setting up LocalAI, you'll need to direct Matcha to the openai-compatible base URL of LocalAI. This is done by updating the "openai_base_url" in the configuration file. For instance, if your LocalAI server is running locally on port 8080, your configuration would look like this:

```yaml
openai_base_url: http://localhost:8080/v1
openai_model: openllama-3b
```

In this case, 'http://localhost:8080/v1' represents the base URL where your LocalAI server is running. 'openai_model' could be any model compatible with LocalAI. You can also replace the openai_base_url with another hosted url like the Azure openai endpoint.
Please note in case of errors that you may need to change the openai_model to match the model you downloaded in LocalAI.

### Command line Options
Run matcha with --help option to see current cli options:
```
  -c filepath
    	Config file path (if you want to override the current directory config.yaml)
  -o filepath
    	OPML file path to append feeds from opml files
  -t	Run Matcha in Terminal Mode, no markdown files will be created
  -check-feeds
    	Fetch every configured feed once and report status codes and item counts (no digest, LLM or database)
```

#### OPML Import
To use OPML files (exported from other services), rename your file to `config.opml` and leave it in the directory where matcha is located. The other option is to run the command with -o option pointing to the opml filepath.
