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

### Recommender (Phase 3)

Rule filter + local text LLM (`06-decisions.md` "Recommender approach").
Code lives in `internal/recommend`; the LLM client talks to `LLM_URL`'s
OpenAI-compatible `/v1/chat/completions`, like the VLM client.

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/api/recommendations` | 3 outfits for today from the catalog |

Request body (both fields optional; empty body `{}` is valid):

```json
{"formality": "smart-casual", "note": "dinner with friends, walking there"}
```

- `formality`: one of `03-taxonomy.md` formality values, or absent/empty for
  any. Anything else is `400` naming `formality`.
- `note`: free text, trimmed, at most 500 characters (`400` naming `note` if
  longer). Passed to the LLM as user context only.

Flow:

1. Fetch today's weather exactly as `GET /api/weather` does. Weather failure
   is `502`; the LLM is not called.
2. Apply the warmth rules (`06-decisions.md` "Weather → warmth thresholds") and
   the formality filter to the catalog to get candidates.
3. If candidates lack any required slot (`top`, `bottom`, `footwear`, and
   `outerwear` when required): `422` with an error naming the missing slot(s).
   The LLM is not called.
4. Prompt the LLM with: weather summary (feels-like, min/max, rain chance,
   condition code), formality, note, and one line per candidate with `id`,
   `category`, `subcategory`, `dominant_color`, `secondary_colors`, `pattern`,
   `warmth_tier`, `formality`. No photos, no `notes` field, no other items.
   The system prompt states the outfit rules and requires JSON only:

   ```json
   {"outfits": [{"item_ids": ["<id>", "..."], "reason": "<one sentence>"}]}
   ```

5. Validate (`06-decisions.md` "Recommender output validation"): exactly 3
   outfits; ids from candidates only; one top, one bottom, one footwear;
   outerwear present when required, absent when excluded, else optional;
   at most one outerwear and at most one headwear; no duplicate ids in an outfit; the 3 id sets are not
   all identical; `reason` non-empty. Invalid → retry once → `502`.

Response `200`:

```json
{
  "weather": { "...": "same shape as GET /api/weather" },
  "outfits": [
    {"items": [{"...": "same item shape as GET /api/items"}], "reason": "Navy and tan are calm for a mild day."}
  ]
}
```

Items within an outfit are ordered: outerwear, top, bottom, footwear,
headwear, accessory.

Errors:

| Case | Status |
|---|---|
| bad JSON body, invalid `formality`, `note` > 500 chars | `400` naming the field |
| candidates cannot fill a required slot | `422` naming the slot(s) |
| weather fetch fails | `502` |
| LLM unreachable, timeout (120 s), non-2xx, or invalid output twice | `502` |
| LLM non-2xx while `LLM_MODEL` is empty | `502`, message says to set `LLM_MODEL` |
| local DB failure | `500` |

UI: a recommendation panel under the weather panel with a formality select
(any + the three taxonomy values), a note text box, and a "Suggest outfits"
button. Results show 3 outfits as rows of garment photos with the reason. The
LLM is only called on click, never on page load. A failure shows the server's
error message and leaves the catalog and weather panel unaffected.

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
│   ├── recommend/               # warmth rules, candidate filter, LLM outfit picker (Phase 3)
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
├── logs/                        # runtime logs, gitignored:
│   ├── app.log                  #   slog output (access, startup, LLM/weather attempts)
│   └── vlm-attempts.jsonl       #   VLM attempt trail (ING-005)
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

Two independent model endpoints are configured. Ingestion calls the VLM; the
Phase 3 recommender calls the LLM — see `06-decisions.md`.

### Env var contract

| Var                      | Required?       | Default | Behavior                                                                                                                                                                                                                                                |
|--------------------------|-----------------|---------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `VLM_URL`                | yes             | —       | startup error if empty. No URL-format validation at startup; a malformed value is accepted and surfaces when a request is attempted as a "VLM unreachable" error (ING-002)                                                                              |
| `VLM_API_KEY`            | no              | —       | opaque string, no client-side format check — empty allowed, no auth header sent; validity is determined by the VLM server's response                                                                                                                    |
| `LLM_URL`                | server only     | —       | `cmd/server` startup error if empty; `cmd/ingest` does not require it. No URL-format validation; a malformed value surfaces as `502` on first recommendation |
| `LLM_API_KEY`            | no              | —       | empty allowed, no auth header sent                                                                                                                                                                                                                      |
| `LLM_MODEL`              | no              | —       | sent as the request `model` when set; omitted when empty. On an LLM non-2xx with this empty, the `502` message says to set `LLM_MODEL` |
| `LLM_TEMPERATURE`        | no              | `0.4`   | same validation as `VLM_TEMPERATURE` (finite, `0.0`–`1.0`, startup error naming the variable otherwise) |
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
### LLM request behavior
- Concurrent, no serialization option (add one only if a real model needs it).
- Fixed 120 s request timeout; timeout is `502`.
- Retry once on invalid output, never on unreachable (`06-decisions.md`).
- Every attempt is logged (see "Logging").

## Logging

Runtime logging uses the Go standard library `log/slog`, with no third-party
logging dependency, keeping the single-embedded-binary runtime model.

- **Sinks:** process stderr and `logs/app.log` (append/create, relative to the
  working directory), both gitignored. `logs/vlm-attempts.jsonl` (ING-005) is
  a separate, structured attempt trail and is unchanged; the two are not
  merged.
- **Level and format:** `LOG_LEVEL` sets the minimum level; `LOG_FORMAT`
  selects a text handler (interactive local runs) or a JSON handler (machine
  parsing). Both apply to stderr and `logs/app.log` alike.
- **Bootstrap logger:** config is loaded before the configured logger exists.
  Config errors are reported through `slog.Default()` on stderr (text format),
  then the process exits with status 1. `slog` has no fatal level; startup
  failures log at `error` and call `os.Exit(1)`.
- **`cmd/ingest`:** diagnostics go to the logger (stderr). Its stdout stays a
  strict one-JSON-object-per-line channel (ING-012).
- **HTTP access log:** every request to the Gin engine logs method, matched
  route, status, latency, response size, client IP and a generated
  `request_id`. The id is always generated server-side and returned as an
  `X-Request-ID` response header; an incoming `X-Request-ID` is ignored.
  Level follows the status class: 2xx/3xx `info`, 4xx `warn`, 5xx `error`.
  Every request is logged, including static assets and photos.
- **Request-scoped logger:** the middleware stores a logger carrying
  `request_id` in the request context. Handlers and the code they call (the
  LLM picker, the weather client) retrieve it from the context, falling back
  to `slog.Default()` when none is present (CLI, tests).
- **Correlation:** error logs carry `request_id` and, where one exists, the
  `item_id`, so an API request can be joined to its
  `logs/vlm-attempts.jsonl` records, which are already keyed by `item_id`.
- **Outbound attempt logging (LLM recommender and Open-Meteo):** one log line
  per attempt with attempt number, HTTP status, elapsed time, response body
  length, outcome and, for failed attempts only, a 300-byte body snippet. The
  full response body is logged only at `debug`. Request bodies are never
  logged; `LLM_API_KEY` is never logged. A successful LLM reply names catalog
  garments, and a successful forecast body begins with the saved location's
  coordinates, so `ok` attempts log the body length only (weather `bad_body`
  does too); request URLs with coordinates appear only at `debug`. Error strings returned to the browser are unchanged. The `502`
  path in `internal/api/recommend.go` logs with the `request_id`.
- **Failure to open `logs/app.log`:** startup logs a warning and continues
  with stderr only; it is not a hard error.
- **No rotation:** `logs/app.log` is not rotated or capped (`06-decisions.md`).
- **Out of scope:** metrics endpoints, distributed tracing, and any
  remote/hosted telemetry. The fully-local hard constraint (`00-overview.md`)
  rules out hosted log/error services outright.
