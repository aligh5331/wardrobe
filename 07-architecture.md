# Architecture

Stack and runtime decisions for the ingestion pipeline and weather signal. See
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

### Weather (Phase 2)

Backend-only Open-Meteo client in `internal/weather`; the browser never calls
Open-Meteo (`06-decisions.md`). No API key, no new env var.

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/weather` | current conditions + today's forecast for the saved location |
| `GET` | `/api/weather/cities?q=<text>` | city search via Open-Meteo geocoding; returns up to 10 `{name, country, admin1, latitude, longitude}` |
| `GET` | `/api/weather/location` | saved location, or the Tehran default |
| `PUT` | `/api/weather/location` | save `{name, country, latitude, longitude}` (`04-data-schema.md`) |

`GET /api/weather` response:

```json
{
  "location": {"name": "Tehran", "country": "Iran", "latitude": 35.69439, "longitude": 51.42151},
  "current": {"temperature_c": 21.3, "apparent_temperature_c": 20.1, "weather_code": 3, "precipitation_mm": 0.0},
  "today": {"temperature_min_c": 14.2, "temperature_max_c": 25.8, "precipitation_probability_max": 10, "weather_code": 3}
}
```

- Fields map 1:1 to Open-Meteo `current=temperature_2m,apparent_temperature,weather_code,precipitation`
  and `daily=temperature_2m_min,temperature_2m_max,precipitation_probability_max,weather_code`
  with `timezone=auto`, `forecast_days=1`. `weather_code` is the raw WMO code;
  mapping it to a label/icon is a frontend concern.
- A `null` or absent value for any requested variable is **missing data, not
  an error**: Open-Meteo returns `null` when the model has no value for that
  variable/location. The field is `null` in the `/api/weather` response (never
  `0`), and the UI shows `-` in its place (`- °C`, `- %`, condition `-`). A
  missing `current` object or empty `daily` arrays is a malformed response
  (`502`).
- Open-Meteo unreachable, non-2xx, or unparseable, on either `/api/weather` or
  `/api/weather/cities`: `502` with an error message. Weather failures never
  affect catalog routes or startup.
- Local DB read/write failure on any weather route: `500`, same as catalog routes.
- `cities`: empty or whitespace-only `q` is `400`; zero results is `200` with `[]`.
  Geocoding is called with `name=<q>&count=10` only (Open-Meteo default
  language); a result missing `country` or `admin1` returns `""` for it.
- `PUT location`: `name` missing, empty, or whitespace-only; `latitude` or
  `longitude` missing (absent is not `0`) or out of range; or malformed JSON
  is `400` with the field named. Success is `200` with the saved location
  `{name, country, latitude, longitude}`.
- Outbound HTTP timeout: 10 s.

UI: a small weather panel showing location name, current temperature, apparent
temperature, condition, and today's min/max and precipitation chance, with a
"change location" city search that saves via `PUT /api/weather/location`.

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
│   └── agents/                  # tracked copy of agents/; what opencode loads.
│                                # Keep in sync with agents/ by hand.
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
│   ├── weather/                 # Open-Meteo forecast + geocoding client (Phase 2)
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
- **`.opencode/agents/` is a tracked copy of `agents/`, not a symlink** —
  symlinks did not survive Windows checkouts, so both directories hold the
  same `*.md` files and git tracks both. `agents/` is canonical; any change to
  it is copied to `.opencode/agents/` in the same commit.
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
## Open / future
- `LLM_URL`/`LLM_API_KEY` have no consumer until Phase 3 (recommender).
  Don't wire up calls to it in Phase 1 or Phase 2 tickets.
