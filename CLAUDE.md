# Matcha (AI CAD fork): notes for Claude Code sessions

Fork of `piqoni/matcha` (a Go RSS digest generator), tuned to follow AI-assisted CAD research and news
(text/image-to-CAD, CadQuery/Build123d/OpenSCAD, design intent, CAD agents). It writes a daily markdown
digest and uses a local LLM (Ollama through the OpenAI-compatible API) plus a local MinerU server to
summarize papers. `README.md` documents every user-facing setting; keep it in sync with the code.

## Build and test
```
go build -o matcha .
go vet ./... && go test ./...      # tests are offline (httptest servers); keep it that way
./matcha -t                        # terminal mode; ./matcha -check-feeds tests every feed once
```
The default `config.yaml` text is the `config` string in `config.go`. The README's first YAML block is a copy
of it. `TestDefaultConfigParses` checks it is valid YAML (a `: ` inside a plain scalar broke it once).

## Run flow (`main.go`)
`LoadConfig` -> (`-check-feeds` exits) -> open SQLite -> weather -> `RunAnalyst` (every analyst) -> `ProcessFeed` per feed.
- `feedfetch.go`: all feed fetching. Matcha User-Agent, 3 s spacing for arXiv hosts, retries on 429/503
  (Retry-After, else 5/20/60 s), `-check-feeds`.
- `processor.go`: per-feed loop, summaries, similar-story clustering hook, empty-section notes, `shownThisRun`
  (items already written this digest are not listed again; arXiv keyed without the version).
- `analysts.go`, `analyst.go`: `analysts:` list, each with own feeds, `google_news` queries, prompt, model, `max_chars`,
  `read_papers`/`max_papers`, and its own section. Items are numbered; the model cites `[n]` and Matcha swaps in the real
  title/link. With `read_papers` it ends with `SELECTED: ..` and picked arXiv papers get full-text summaries.
  `arxiv_searches` with `use: analyst` go to the analyst named by `analyst:` (else the first). Bookkeeping `<url>#analyst:<slug>`.
  The old top-level `analyst_*` keys still work as a first analyst "Daily Analysis" (bookkeeping `<url>#analyst`).
  `notification_trigger` applies to the first analyst. Defaults: "CAD research scout" and "Creator manufacturing demand"
  (HN/trade press/Alerts/Google News; feed URLs unverified). Reddit was dropped: the author's `-check-feeds` got 403 on every Reddit search feed.
- `openalex.go`, `citation.go`: `citation_watch` (new papers citing chosen papers) and `-anchors` (references shared by the watched/saved
  papers), both via OpenAlex. A watch is a pseudo feed URL `openalex-cites://watch?ref=..&days=..` that `fetchFeed` answers, so it works in
  listed sections, analysts and `-check-feeds`. `Storage.IsSeen` also matches other spellings of an arXiv link (`http`/`https`, with/without
  `vN`) so OpenAlex's `https://arxiv.org/abs/<id>` and the arXiv API's `http://arxiv.org/abs/<id>v1` are one paper.
- `arxiv.go`: `arxiv_searches` -> arXiv query-API feed URLs (terms/categories/exclude), sections titled `arXiv: <name>`.
- `mineru.go`, `mineru_tasks.go`, `paper.go`, `paperstore.go`: MinerU client (4.1 Tasks API in `mineru_tasks.go`), reference/appendix trimming, code-link extraction,
  on-disk paper store (`paper_store_dir`).
- `fetch.go`, `googlenews.go`: article fetching (HTTP/1.1, browser UA, anchor targets), Google News link
  decoding, Google Alerts `google.com/url?url=` unwrapping.
- `cluster.go`: one summary per cluster of near-duplicate headlines (strict: >=3 shared uncommon words, >=40% overlap).
- `migrations.go`: SQLite tables `seen(url,date,summary,title,feed_title)` and `notifications`. Dates are local
  time `YYYY-MM-DD`; use `date('now','localtime')` in SQL, not `date('now')`. Analyst bookkeeping uses `<url>#analyst`.

## Facts learned the hard way
- **MinerU 4.1** (the author runs 4.1.7 since 2026-11-09) replaced the job API with the Tasks API (`/v1/tasks`, PUT to
  `files[].upload.url`, result ZIP at `files[].result_url` holding `markdown.md`); the old routes return 404. `mineru_tasks.go`
  implements it. `probe()` in `mineru.go` picks Tasks (health has `features.task_output_formats` or version >= 4.1), the 3.x/4.0
  job API (`/v1/uploads`, `/v1/parse/jobs`), or legacy `/file_parse` (no `/v1/health`). `mineru-openapi.json` is the 4.0 spec,
  not 4.1. The 4.1 contract was read from the `mineru` 4.1.7 wheel (`mineru/tasks/http.py`, `mineru/parser/api_client.py`).
- **arXiv PDFs cannot be parsed by URL job**: `/pdf/<id>.pdf` 301-redirects to `/pdf/<id>`, MinerU does not follow
  redirects and rejects the extensionless name. Matcha downloads the PDF itself and uploads it.
- **Reddit** RSS searches answered 403 (and 429 back to back) from the author's machine in Nov 2026; after two 403s from a host in a run, `fetchFeed` skips that host's remaining feeds.
- **arXiv API** asks for one request per 3 s; `429` was seen in practice.
- **Google News** `news.google.com/rss/articles/CBMi...` IDs are encrypted and no longer redirect. `googlenews.go` asks
  Google's undocumented `batchexecute` endpoint for the publisher URL; it may break without notice.
- **arXiv category RSS** (`rss.arxiv.org/rss/cs.CV`) lists a whole announcement (300 to 500 items) and is empty between
  announcements; the `URL N` limit cuts it before the Analyst sees it. Empty analyst feeds are reported in the Analyst note.
- **gemma4 via Ollama** has leaked its thinking into the reply (a draft list, then `<channel|>`, then the answer).
  `stripReasoning` in `llm.go` keeps the text after the last `<channel|>` or `</think>`.
- **llm-for-zotero** keeps its own MinerU cache keyed by Zotero attachment ID; Matcha's paper store is separate.
- **ChinaXiv** (Chinese arXiv) documents an OAI-PMH endpoint, but it answered "no right to access" from the author's
  machine; not implemented. OAI-PMH has no keyword search, so it would need date-based harvesting plus local filtering.
- Platform note: a laptop with the lid closed sleeps even on AC, and `cron`/`launchd` do not run during sleep. DarkWake
  windows are about 3 minutes. The Go binary has no scheduler and creates no lock files;
  `scripts/macos/` (install/uninstall/run-daily) adds a LaunchAgent (00:05 plus a 15-minute catch-up), a
  `pmset repeat wakeorpoweron` wake (sudo) and `caffeinate -i -s`, with a once-a-day stamp file. The author's own Mac runs
  Matcha from a separate LaunchAgent outside this repo with the same values; the repo scripts are not verified on a real Mac.

## Verified by the author vs not
Reported working: MinerU job flow and paper summaries, Google News decoding, similar-story clustering.
Not yet verified on a real setup: the OpenAlex client (`citation_watch`, `-anchors`; built from the OpenAlex docs and a mock server, the sandbox cannot reach `api.openalex.org` unless that host is allowed), the MinerU 4.1 Tasks client, `paper_store` (including `paper_store_zip`), the 429 retry/pacing behaviour,
the arXiv download-and-upload path, `-check-feeds`. Say so when describing them.

## Working agreements
- Develop on `claude/jolly-hawking-kuvb2z`. PRs are merged by the author; after a merge, fast-forward the branch
  to `main` (`git merge --ff-only origin/main`) and open a new PR for new work. No force pushes; merge, don't rebase.
- Every new setting gets a README entry and a test. Do not claim something works on the author's machine unless
  they confirmed it; the dev sandbox cannot reach most hosts (arXiv, Google, ChinaXiv).
- Do not put API keys, Alert feed URLs or personal paths in the repo.

## Open items
- Keying seen items on the unwrapped publisher URL so one article from two Alerts is listed once.
