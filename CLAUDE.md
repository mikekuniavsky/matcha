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
`LoadConfig` -> (`-check-feeds` exits) -> open SQLite -> weather -> `RunAnalyst` -> `ProcessFeed` per feed.
- `feedfetch.go`: all feed fetching. Matcha User-Agent, 3 s spacing for arXiv hosts, retries on 429/503
  (Retry-After, else 5/20/60 s), `-check-feeds`.
- `processor.go`: per-feed loop, summaries, similar-story clustering hook, empty-section notes, `shownThisRun`
  (items already written this digest are not listed again; arXiv keyed without the version).
- `analyst.go`: LLM screens titles+abstracts from `analyst_feeds` and `arxiv_searches` (`use: analyst`). Items are
  numbered; the model cites `[n]` and Matcha swaps in the real title/link. With `analyst_read_papers` it ends
  with `SELECTED: ..` and picked arXiv papers get full-text summaries.
- `arxiv.go`: `arxiv_searches` -> arXiv query-API feed URLs (terms/categories/exclude), sections titled `arXiv: <name>`.
- `mineru.go`, `paper.go`, `paperstore.go`: MinerU client, reference/appendix trimming, code-link extraction,
  on-disk paper store (`paper_store_dir`).
- `fetch.go`, `googlenews.go`: article fetching (HTTP/1.1, browser UA, anchor targets), Google News link
  decoding, Google Alerts `google.com/url?url=` unwrapping.
- `cluster.go`: one summary per cluster of near-duplicate headlines (strict: >=3 shared uncommon words, >=40% overlap).
- `migrations.go`: SQLite tables `seen(url,date,summary,title,feed_title)` and `notifications`. Dates are local
  time `YYYY-MM-DD`; use `date('now','localtime')` in SQL, not `date('now')`. Analyst bookkeeping uses `<url>#analyst`.

## Facts learned the hard way
- **MinerU 3.x** exposes a job API (`/v1/uploads`, `/v1/parse/jobs`, `/v1/files`), not `/file_parse`. Matcha probes
  `/v1/health` and falls back to `/file_parse` for old servers. The spec the author's server serves is in `mineru-openapi.json`.
- **arXiv PDFs cannot be parsed by URL job**: `/pdf/<id>.pdf` 301-redirects to `/pdf/<id>`, MinerU does not follow
  redirects and rejects the extensionless name. Matcha downloads the PDF itself and uploads it.
- **arXiv API** asks for one request per 3 s; `429` was seen in practice.
- **Google News** `news.google.com/rss/articles/CBMi...` IDs are encrypted and no longer redirect. `googlenews.go` asks
  Google's undocumented `batchexecute` endpoint for the publisher URL; it may break without notice.
- **llm-for-zotero** keeps its own MinerU cache keyed by Zotero attachment ID; Matcha's paper store is separate.
- **ChinaXiv** (Chinese arXiv) documents an OAI-PMH endpoint, but it answered "no right to access" from the author's
  machine; not implemented. OAI-PMH has no keyword search, so it would need date-based harvesting plus local filtering.
- Platform note: a laptop with the lid closed sleeps even on AC, and `cron`/`launchd` do not run during sleep. DarkWake
  windows are about 3 minutes. Matcha has no scheduler of its own and creates no lock files.

## Verified by the author vs not
Reported working: MinerU job flow and paper summaries, Google News decoding, similar-story clustering.
Not yet verified on a real setup: `paper_store` (including `paper_store_zip`), the 429 retry/pacing behaviour,
the arXiv download-and-upload path, `-check-feeds`. Say so when describing them.

## Working agreements
- Develop on `claude/jolly-hawking-kuvb2z`. PRs are merged by the author; after a merge, fast-forward the branch
  to `main` (`git merge --ff-only origin/main`) and open a new PR for new work. No force pushes; merge, don't rebase.
- Every new setting gets a README entry and a test. Do not claim something works on the author's machine unless
  they confirmed it; the dev sandbox cannot reach most hosts (arXiv, Google, ChinaXiv).
- Do not put API keys, Alert feed URLs or personal paths in the repo.

## Open items
- Scheduling on macOS: wake the Mac shortly before the run (`sudo pmset repeat wakeorpoweron ...`) and run Matcha under
  `caffeinate -i -s`. Needs the run time and the launcher type from the author. Possible Matcha-side helpers: a
  `-once-per-day` flag and a start/finish footer in each digest.
- Keying seen items on the unwrapped publisher URL so one article from two Alerts is listed once.
