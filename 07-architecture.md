# Architecture

Stack and runtime decisions for the ingestion pipeline. See
`06-decisions.md` for the why; this file is the concrete contract a
coding agent implements against.

## Runtime model
Single Go binary, run locally, opened via browser tab at `localhost`.
No native desktop wrapper (no Wails/Electron), no separate Node
process. The Vite frontend build is embedded into the Go binary with
`go:embed` — one executable serves both the API and the UI.

## Backend
- **Language:** Go
- **Router:** Gin
- **ORM/DB access:** GORM, `sqlite` driver
- **DB:** SQLite, single file under `data/wardrobe.db`
- **Image storage:** filesystem, under `data/photos/`; path stored in
  the item's `photo_path` field per `04-data-schema.md`

### Catalog write API (interactive create/edit)

The UI write path from `06-decisions.md` ("Interactive catalog create/edit
UI"). All routes are local, no auth:

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/api/items/photo` | multipart photo upload; stages the file, runs the local VLM tagging pipeline, returns a **non-persisted draft** (tagging fields + a photo reference) |
| `POST` | `/api/items` | persist a confirmed draft: move the staged photo into `data/photos/`, insert the catalog row |
| `GET` | `/api/items/:id` | one item, for the edit form |
| `PUT` | `/api/items/:id` | update the mutable fields (all tagging fields + `notes`) |

- Validation reuses the taxonomy tables already in `internal/tagging`
  (`ParseTaggingResult`): an invalid field is `400` with the field named, an
  unknown id is `404`.
- `id`, `added_date`, and the photo are immutable via `PUT`.
- No delete route in Phase 1.
- Staged uploads live in a gitignored runtime staging directory under `data/`
  and are removed on save or failed create (`06-decisions.md`).

### Taxonomy read route (create/edit forms)

`GET /api/taxonomy` returns the closed vocabulary the forms must offer —
categories with their valid subcategories, the color palette, patterns, warmth
tiers, and formality — derived from the same tables `internal/tagging`
validates against. It exists so the browser does not hand-duplicate
`03-taxonomy.md` (`06-decisions.md`).

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/taxonomy` | closed enum vocabulary for the create/edit forms |

## Frontend
- **Framework:** React, built with Vite (SPA, not Next.js — no SSR/API
  routes needed for a single-user localhost app)
- **Styling:** Tailwind v4
  - Tailwind v4 setup differs from v3 — no `tailwind.config.js` by
    default (theme config lives in CSS via `@theme`), no separate
    PostCSS/autoprefixer config, single `@import "tailwindcss";` entry
    point, and the `@tailwindcss/vite` plugin instead of the old
    PostCSS pipeline. Flagging explicitly so a coding agent doesn't
    default to v3-era scaffolding.
- **Build output:** `frontend/dist/`, embedded into the Go binary at
  build time

## Full project structure

```
wardrobe/
├── 00-overview.md               # spec docs stay flat at repo root —
├── 01-agentic-workflow.md       # agent permission globs (0*.md) already
├── 02-agile-process.md          # assume this location; moving them into
├── 03-taxonomy.md               # a docs/ folder would mean updating every
├── 04-data-schema.md            # agent's permission block to match
├── 05-vlm-tagging-spec.md
├── 06-decisions.md
├── 07-architecture.md
│
├── agents/                      # canonical role definitions (single source)
│   ├── planner.md
│   ├── coder.md
│   ├── tester.md
│   └── reviewer.md
│
├── .opencode/
│   └── agents -> ../agents      # symlink to agents/ (Linux); what opencode
│                                # loads. Git stores it as a symlink (120000);
│                                # on Windows it may check out as a plain file,
│                                # so keep the two in sync by hand there.
│
├── backlog/                     # one file per ticket, board state via
│   └── <TICKET-ID>.md           # a `**Status:**` line (02-agile-process.md)
│
├── cmd/
│   └── server/
│       └── main.go              # Gin server, embeds frontend/dist
│
├── internal/
│   ├── tagging/                 # VLM client, serialization, taxonomy validation
│   ├── catalog/                 # shared photo+row persistence (CLI + API)
│   ├── store/                   # GORM models + sqlite access
│   └── api/                     # Gin handlers
│
├── frontend/
│   ├── src/
│   ├── dist/                    # Vite build output — generated, gitignored
│   ├── package.json
│   └── ...
│
├── data/                        # runtime, personal, gitignored entirely
│   ├── wardrobe.db              # your actual catalog + your garment photos —
│   ├── photos/                  # never belongs in git history
│   └── ingest-staging/          # transient UI uploads; removed on save/failure
│
├── logs/                        # runtime logs (startup warnings — e.g. the
│   └── ...                      # VLM_REQUEST_DELAY_MS misconfig warning —
│                                 # request/error logs). Gitignored.
│
├── temp/                        # Coder/Tester scratch space, ask-gated per
│   └── ...                      # agent permissions (06-decisions.md). Gitignored.
│
├── .env                         # VLM_URL/LLM_URL/API keys etc. Gitignored.
├── .env.example                 # tracked template, values left blank
├── go.mod
├── go.sum
└── .gitignore
```

Judgment calls baked into this layout, worth revisiting if you want it
different:
- **Specs stay flat at root**, not moved into `docs/` — every agent's
  permission block already hardcodes `0*.md` as a glob (deny-edit for
  Planner/Tester/Reviewer, deny for Coder). Moving them means updating
  four permission blocks to `docs/0*.md` for no functional gain.
- **`.opencode/agents/` is a symlink to `agents/`, not a second copy** — on
  Linux it resolves to the same `agents/*.md`, so the canonical role files are
  the single source of truth and there is nothing to keep in sync. Git tracks
  it as a symlink (mode `120000`), not as duplicated file contents. Windows
  checkouts may not honor the link — git can materialize it as a plain file
  holding the target path — so on Windows the `agents/` and `.opencode/agents/`
  copies must be kept in sync by hand. That's a machine/platform caveat, not a
  project convention.
- **`data/` is fully gitignored**, db included — it's real wardrobe
  photos and personal cataloging data, not something to commit even
  privately.

## VLM / LLM connectivity

Two independent model endpoints are configured. Phase 1 ingestion only
calls the VLM; the LLM endpoint is provisioned ahead of use for Phase 3
(outfit recommender) — see `06-decisions.md`.

### Env var contract

| Var                      | Required?       | Default | Behavior                                                                                                                                                                                                                                                |
|--------------------------|-----------------|---------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `VLM_URL`                | yes             | —       | startup error if empty. No URL-format validation at startup; a malformed value is accepted and surfaces when a request is attempted as a "VLM unreachable" error (ING-002)                                                                              |
| `VLM_API_KEY`            | no              | —       | opaque string, no client-side format check — empty allowed, no auth header sent; validity is determined by the VLM server's response                                                                                                                    |
| `LLM_URL`                | yes (once used) | —       | startup error if empty; no consumer in Phase 1                                                                                                                                                                                                          |
| `LLM_API_KEY`            | no              | —       | empty allowed, no auth header sent                                                                                                                                                                                                                      |
| `VLM_SERIALIZE_REQUESTS` | no              | `false` | boolean parsed with `strconv.ParseBool` (`1/t/T/TRUE/true/True`, `0/f/F/FALSE/false/False`); unset or empty is `false`; any other value is a startup error naming the variable. `true` forces a global one-at-a-time queue/mutex around all VLM calls   |
| `VLM_REQUEST_DELAY_MS`   | no              | `0`     | integer; negative values are clamped to 0 with a startup warning; non-numeric is a startup error naming the variable. If >0, wait this long after each VLM response before sending the next request. Only meaningful when `VLM_SERIALIZE_REQUESTS=true` |
| `VLM_TEMPERATURE`        | no              | `0.4`   | sampling temperature sent on each VLM tagging request; finite number in `0.0`–`1.0` inclusive. Anything else (non-numeric, NaN/Inf, negative, or >1.0) is a startup error naming the variable. `0.0` is valid for deliberate deterministic runs         |
| `LOG_LEVEL`              | no              | `info`  | minimum level emitted; one of `debug`, `info`, `warn`, `error` (case-insensitive). Anything else is a startup error naming the variable                                                                                                                |
| `LOG_FORMAT`             | no              | `text`  | handler format; one of `text`, `json` (case-insensitive). `text` for interactive local runs, `json` for machine parsing. Anything else is a startup error naming the variable                                                                            |

### VLM request behavior
- **Default:** concurrent requests to `VLM_URL`, no artificial
  bottleneck — the app shouldn't stall on a single-threaded queue when
  there's no known issue with the model in use.
- **Opt-in workaround:** setting `VLM_SERIALIZE_REQUESTS=true` switches
  the backend to a global serialized queue — one in-flight VLM request
  at a time, for VLMs with known parallel-request problems (e.g.
  llama.cpp issue `#17200` against Qwen3-VL).
- **Delay on top of serialization:** `VLM_REQUEST_DELAY_MS` (e.g. `15`)
  adds a wait after each response before the next request is sent.
  Only takes effect when serialization is on.
- **Misconfiguration handling:** if `VLM_REQUEST_DELAY_MS>0` while
  `VLM_SERIALIZE_REQUESTS=false`, the backend logs a **startup
  warning** and proceeds with no delay applied (not a hard error).
- **Negative delay:** a negative `VLM_REQUEST_DELAY_MS` is treated as `0`
  and logged as a **startup warning** — not stored as-is, not a hard error.

## Logging

Runtime logging uses the Go standard library `log/slog` — no third-party
logging dependency, keeping the single-embedded-binary runtime model.

- **Sinks:** process stderr and `logs/app.log` (append/create), both
  gitignored. `logs/vlm-attempts.jsonl` (ING-005) is a separate, structured
  attempt trail and is unchanged; the two are not merged.
- **Level and format:** `LOG_LEVEL` sets the minimum level; `LOG_FORMAT`
  selects a text handler (interactive local runs) or a JSON handler
  (machine parsing). Both apply to stderr and `logs/app.log` alike.
- **HTTP access log:** every request to the Gin engine logs method, matched
  route, status, latency, response size, client IP, and a generated
  `request_id`. The id is also returned to the client as an `X-Request-ID`
  response header. Level follows the status class: 2xx/3xx `info`, 4xx
  `warn`, 5xx `error`.
- **Correlation:** server-side error logs carry the access log's
  `request_id` and, where one exists, the tagged `item_id` — so an API
  request can be joined to its `logs/vlm-attempts.jsonl` records, which are
  already keyed by `item_id`.
- **Failure to open `logs/app.log`:** startup logs a warning and continues
  with stderr only; it is not a hard error.
- **Out of Phase 1:** metrics endpoints, distributed tracing, and any
  remote/hosted telemetry. The fully-local hard constraint (`00-overview.md`)
  rules out hosted log/error services outright.

## Open / future
- `LLM_URL`/`LLM_API_KEY` have no consumer until Phase 3 (recommender).
  Don't wire up calls to it in Phase 1 tickets.
