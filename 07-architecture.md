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
├── agents/                      # canonical role definitions (source of truth)
│   ├── planner.md
│   ├── coder.md
│   ├── tester.md
│   └── reviewer.md
│
├── .opencode/
│   └── agents/                  # wrapped copies opencode actually loads
│       ├── planner.md           # (YAML frontmatter + permissions block),
│       ├── coder.md             # generated from agents/*.md — tracked in
│       ├── tester.md            # git, not gitignored, since the permission
│       └── reviewer.md          # blocks are security-relevant and worth
│                                 # diffing in history
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
│   └── photos/                  # never belongs in git history
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
- **`.opencode/agents/` is tracked, not ignored** — it's generated from
  `agents/*.md`, but it's also where the actual enforced permissions
  live, so having it in git history (and diffable) matters more than
  treating it as a build artifact.
- **`data/` is fully gitignored**, db included — it's real wardrobe
  photos and personal cataloging data, not something to commit even
  privately.

## VLM / LLM connectivity

Two independent model endpoints are configured. Phase 1 ingestion only
calls the VLM; the LLM endpoint is provisioned ahead of use for Phase 3
(outfit recommender) — see `06-decisions.md`.

### Env var contract

| Var | Required? | Default | Behavior |
|---|---|---|---|
| `VLM_URL` | yes | — | startup error if empty |
| `VLM_API_KEY` | no | — | empty allowed, no auth header sent |
| `LLM_URL` | yes (once used) | — | startup error if empty; no consumer in Phase 1 |
| `LLM_API_KEY` | no | — | empty allowed, no auth header sent |
| `VLM_SERIALIZE_REQUESTS` | no | `false` | `true` forces a global one-at-a-time queue/mutex around all VLM calls |
| `VLM_REQUEST_DELAY_MS` | no | `0` | if >0, wait this long after each VLM response before sending the next request. Only meaningful when `VLM_SERIALIZE_REQUESTS=true` |

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

## Open / future
- `LLM_URL`/`LLM_API_KEY` have no consumer until Phase 3 (recommender).
  Don't wire up calls to it in Phase 1 tickets.
