# Decisions

Newest first. Each entry: decision, date-ish context, why.

## Structured logging: stdlib `log/slog`, local stderr + file, env-configurable level/format

The backend's logging was ad hoc: stdlib `log.Printf`/`log.Fatalf` in two
entrypoints, a GORM stderr adapter, and no HTTP request logging at all
(`gin.New()` + `gin.Recovery()`, no logger middleware). VLM attempts already
had their own JSONL trail (`logs/vlm-attempts.jsonl`, ING-005). The two
outbound dependencies added later, the LLM and Open-Meteo, left no trace when
they failed; the `stream` bug below needed a manual curl to diagnose. The goal
is that any failure point in the app can be observed from local logs.

Decision:

- **Logger:** Go's standard library `log/slog`. No third-party logging
  dependency, so the single-embedded-binary runtime model
  (`07-architecture.md`) is untouched.
- **Sinks:** process stderr and `logs/app.log` (append), both gitignored,
  relative to the working directory. `logs/vlm-attempts.jsonl` stays separate
  and unchanged. LLM and weather attempts go to `app.log` under their own
  message names; they do not get a separate JSONL file.
- **Config:** `LOG_LEVEL` (`debug`/`info`/`warn`/`error`, default `info`) and
  `LOG_FORMAT` (`text`/`json`, default `text`), validated at startup in the
  same strict style as the other env vars: an unrecognized value is a startup
  error naming the variable, not a silent fallback. No `LLM_DEBUG` variable;
  full LLM response bodies are a `debug`-level log instead.
- **Bootstrap logger:** config loads before the configured logger exists, so
  config errors (including an invalid `LOG_LEVEL`) are reported through
  `slog.Default()` on stderr in text format. That is the only case where
  `LOG_FORMAT=json` is not honored.
- **Fatal exits:** `slog` has no fatal level. Startup failures log at `error`
  and then call `os.Exit(1)`.
- **Access logging:** one Gin middleware logs method, matched route, status,
  latency, response size, client IP and a generated `request_id`, at a level
  chosen from the status class. The id is always generated server-side (an
  incoming `X-Request-ID` is ignored) and returned as `X-Request-ID`. Every
  request is logged, including static assets and photo requests; filtering is
  a later change if the noise becomes a problem.
- **Correlation:** the middleware attaches a request-scoped logger carrying
  `request_id` to the request context; handlers and the code they call
  retrieve it, so server-side error logs join to the access log line. Where a
  tagged item exists the logs also carry `item_id`, joining an API request to
  its VLM attempt records.
- **Outbound attempt logging (LLM and Open-Meteo):** one log line per attempt
  with attempt number, HTTP status, elapsed time, response body length,
  outcome, and a 300-byte snippet of the body. The full response body is
  logged only at `debug`. Request bodies (which hold wardrobe data) are never
  logged. `LLM_API_KEY` is never logged. Error strings returned to the browser
  are unchanged; snippets go to server logs only.
- **Robustness:** failing to open `logs/app.log` warns and falls back to
  stderr only; it does not block startup.
- **Known ceiling:** `logs/app.log` has no rotation or size cap. At
  single-user volume and default `info` level this is acceptable; `debug`
  grows faster and includes response bodies, which can contain wardrobe data.
  Add rotation only if the file becomes a problem.

Rejected:

- **A third-party logger (`zap`/`zerolog`):** performance is irrelevant at
  single-user localhost scale, and `slog` is stdlib.
- **`gin.Logger()` as-is:** writes an unstructured line to stdout; the ingest
  CLI's stdout is a strict one-JSON-object-per-line channel (ING-012), and the
  format carries none of the correlation fields.
- **`LLM_DEBUG` env var:** superseded by the `debug` log level.
- **Prometheus `/metrics`:** a sizeable dependency tree and a new route, and
  it only pays off once something scrapes it; nothing does locally today.
- **OpenTelemetry tracing:** one local process and single HTTP hops to the
  VLM, LLM and weather service; a collector/backend is pure overhead.
- **Hosted error tracking (Sentry et al.):** violates the fully-local hard
  constraint (`00-overview.md`) outright. Open-Meteo stays the only external
  service.

## Agent step caps raised; tickets should be sized to fit a subagent's step budget

`.opencode/agents/*.md` (and their tracked source `agents/*.md`) each set a
`steps:` cap: the number of tool-call rounds a session gets before opencode
forces a stop. Coder was `30`, tester `25`, planner and reviewer `15` each.

Raised to coder `60`, tester `50`, planner `30`, reviewer `30`.

Why: on Claude models with extended thinking (`cc/claude-sonnet-5`,
`cc/claude-opus-5-5` via 9router), hitting the step cap mid-task made
opencode inject a forced-stop message with an `assistant` role as the last
message in the conversation, then immediately resend it. Anthropic rejects
any request ending in an assistant-role message when thinking is enabled
("assistant message prefill... conversation must end with a user message"),
so the resend came back as a hard 400 and killed the whole session, losing
whatever the subagent had done in the reached steps. This is a known,
unfixed opencode bug (`anomalyco/opencode#32548`): the injected message has
the wrong role for thinking-enabled Claude models. It cannot be worked
around from a plugin — no hook runs late enough to intercept or drop that
injected message before it is sent.

Since the bug can't be fixed from this repo, the only available mitigation is
avoiding the step cap in the first place: raise the ceiling, and keep
individual coder/tester/reviewer/planner invocations small enough that a
normal ticket finishes well under it. A single subagent call that tries to
read the spec, implement, validate, and write notes for a multi-file ticket
in one shot is exactly the shape that runs long enough to hit this. Prefer
splitting a large ticket's work across more than one subagent turn (e.g. read
and confirm scope first, then implement) over relying on a larger step
budget alone — a bigger cap buys headroom, it does not make it safe to hand a
subagent an unbounded task.

Rejected: leaving the caps as-is (too easy to trip on a normal-sized ticket)
and disabling extended thinking to route around the prefill restriction
(thinking is a model quality trade-off unrelated to this bug and not ours to
disable per-request from agent config).

See `AGENTS.md` "Read before changing" / "Scope and phase discipline" for the
related instruction to keep task delegation appropriately scoped.

## Phase 3 scope = outfit recommender; Phase 2 closed

Phase 2 (ING-041..ING-045) is done and in use. Phase 3 builds Layer 3 from
`00-overview.md`: on a button click, suggest **3 outfits** for today, each with
a one-line reason. Inputs: today's weather (Phase 2), an optional formality
(`03-taxonomy.md`), and optional free text from the user.

An outfit is **top + bottom + footwear** (required), plus **outerwear** when
the weather rule requires it, plus optional headwear/accessories.

Not in scope: outfit log / "I wore this", ratings, garment embeddings / vector
search, outfit image generation (all stay in `later-ideas.md`), multi-day
planning, recommending for a date other than today.

## Recommender approach: rule filter retrieves candidates, local LLM picks

Two steps:

1. **Deterministic filter (Go):** today's weather maps to allowed warmth tiers
   and whether outerwear is required (thresholds below); the catalog is
   filtered by those tiers and by formality when one is chosen. This is the
   retrieval step — plain SQL/Go over the catalog, no vectors.
2. **Local text LLM (`LLM_URL`):** receives only the candidates' ids and tags
   (no photos, no notes), the weather, formality, and user text, and returns 3
   outfits as strict JSON. It does the part rules are bad at: color
   coordination and interpreting the free text.

Why: a pure rule engine cannot read "wedding" or "long walk" and color
matching by hand-written rules is brittle; sending the whole catalog to the
LLM invites weather-inappropriate picks and a larger prompt. Filtering first
keeps the LLM's job small and its hard failures (wrong warmth) impossible.

Rejected for now: embedding-based RAG (FashionCLIP + `sqlite-vec`). A catalog
of a few hundred items needs no vector search, and the valuable retrieval
target — highly rated past outfits — needs the outfit log, which is not built.
Revisit together with the outfit log (`later-ideas.md`).

## Weather → warmth thresholds: starting values, to be tuned from real use

Based on `current.apparent_temperature_c` (feels-like):

| Feels-like | Allowed warmth tiers | Outerwear |
|---|---|---|
| < 10 °C | `medium`, `heavy` | required |
| 10–20 °C (inclusive) | `light`, `medium` | optional |
| > 20 °C | `light` | excluded |

- If today's `temperature_min_c`..`temperature_max_c` range crosses a
  threshold, the tiers of both bands are allowed (layering for the day).
  The outerwear rule does not widen: it follows the band of the feels-like
  value (or its fallback below) only.
- `precipitation_probability_max >= 50` is passed to the LLM as a hint only,
  never a hard filter.
- Warmth filtering applies to `top`, `bottom`, `outerwear`, `footwear`.
  `headwear` and `accessory` are not warmth-filtered.
- If feels-like is `null` (Phase 2 missing data), fall back to the midpoint of
  today's min/max (if only one of min/max is present, use it); if both are
  also `null`, apply no warmth filter, treat outerwear as optional, and tell
  the LLM the temperature is unknown.
- The formality filter, when a formality is chosen, applies to every
  category (items must match exactly).

These numbers are deliberately simple defaults, kept as named constants in one
place. Ali will tune them from real use; changing them is a spec edit to this
entry, not a new env var.

## Recommender output validation: retry once, then 502

The LLM's JSON is untrusted, same as VLM tagging output. Every outfit must:
use only candidate ids; contain exactly one `top`, one `bottom`, one
`footwear`; contain one `outerwear` when required and none when excluded; have
no duplicate ids within the outfit; and the 3 outfits must not be identical
sets. Invalid or unparseable output is retried **once** (same prompt, nonzero
temperature, as in the VLM policy); a second failure is `502`. An unreachable
LLM is `502` immediately with no retry. If the candidates cannot fill a
required slot, respond `422` naming the slot **without** calling the LLM.

## LLM config: `LLM_URL` required by the server only; optional `LLM_MODEL`; fixed 120 s timeout

- `LLM_URL` becomes a startup error **in `cmd/server` only**. `cmd/ingest`
  never calls the LLM and keeps starting without it.
- `LLM_TEMPERATURE` (optional, default `0.4`, `0.0`–`1.0`) mirrors
  `VLM_TEMPERATURE` validation.
- `LLM_MODEL` (optional). Empty: no `model` field is sent (llama.cpp ignores
  it). Set: sent as-is. Needed for servers such as Ollama that require a model
  name. When `LLM_MODEL` is empty and the LLM server rejects the request (any
  non-2xx), the `502` message tells the user to set `LLM_MODEL`.
- LLM requests time out after a fixed **120 s** (`502`). Not configurable;
  add an env var only if a real model needs longer.

## LLM request always sends `"stream": false` (fixed directly, no ticket)

Found in manual testing against a 9router gateway in front of Claude Sonnet.
The picker did not send a `stream` field, and 9router streams by default, so
the reply was `text/event-stream` (`data: {...}`) instead of a chat envelope.
Both attempts failed with `response is not valid JSON: invalid character 'd'`,
which became a `502`. llama.cpp defaults to non-streaming, so the local
setup never showed it.

The request now carries `"stream": false` explicitly. One field in
`internal/recommend/picker.go` plus one assertion in `TestPickRequestShape`.
It changes no contract, no config and no behavior for servers that already
answered in one piece, so it was too small for a ticket and was done as a
direct fix by Ali's call. SSE parsing is not supported and not planned.
Logging of LLM attempts is specified in "Structured logging: stdlib
`log/slog`" above.

## Phase 2 scope = weather signal; Phase 1 closed

Phase 1 (ING-001..ING-037) is done and in use. Phase 2 builds Layer 2 from
`00-overview.md`: weather for one location, shown in the UI and available to
the Phase 3 recommender later. Supersedes "Phase 1 scope = ingestion pipeline
only" below for current scope; that entry stays as history.

In scope: current conditions + today's daily forecast, a location picker with
city search in the web UI, persisted location. Not in scope: multi-day
forecasts, multiple saved locations, weather history, alerts, any outfit logic
(Phase 3), any use of `LLM_URL`.

## Weather provider: Open-Meteo, backend-proxied, no API key

Open-Meteo forecast (`api.open-meteo.com/v1/forecast`) and geocoding
(`geocoding-api.open-meteo.com/v1/search`) APIs. Free, no key, no account, so
no new secret in `.env`. The Go backend makes every Open-Meteo call; the
browser only talks to `localhost`. Keeps the one external dependency in one
package (`internal/weather`), testable with `httptest`, and keeps the SPA free
of third-party origins.

Data leaving the machine: the saved location's coordinates and city search
text. Never catalog data or photos. This is the scoped exception already
recorded in "Weather signal is an acknowledged exception to 'fully local'".

No cache in Phase 2: one fetch per `GET /api/weather`, well under Open-Meteo's
free-tier limits for one user. Add a short TTL cache if the recommender starts
calling it per request.

Rejected: OpenWeatherMap/WeatherAPI (API key required); calling Open-Meteo
directly from the browser (second network surface, CORS-dependent, untestable
from Go).

## Weather location: chosen in the web UI, persisted in SQLite, default Tehran

Location is set by the user through a city search in the UI, not by env vars.
It is stored in SQLite (`04-data-schema.md` "Settings — weather location") so
it survives restarts and travels with the rest of the install's data. Until
the user picks one, the default is **Tehran, Iran** (`35.69439, 51.42151`), so
weather always has a location and there is no "unconfigured" state to handle.

Rejected: `WEATHER_CITY` / `LAT` / `LONG` env vars with precedence rules — a
restart-to-change setting for something the user changes from the UI, plus a
precedence/conflict path that only exists because of two config sources.

## Taxonomy exported to the browser via `GET /api/taxonomy`, not a bundled copy

The interactive create/edit forms need the valid enum values, but
`03-taxonomy.md` is a repo spec, not shipped to the frontend, and
`07-architecture.md` exposed no taxonomy route. Rather than duplicating the
enums into the JS bundle, the backend serves them: `GET /api/taxonomy` returns
categories with their valid subcategories, the color palette, patterns, warmth
tiers, and formality — derived from the same tables `internal/tagging`
validates against, so the client and server cannot drift and a future taxonomy
change reaches the forms without a frontend edit.

Rejected: a generated or hand-copied client-side enum module — `03-taxonomy.md`
is already mirrored into `internal/tagging/validate.go`; a third copy is the
one that silently goes stale.

## Interactive catalog create/edit UI: local VLM draft, human confirm, shared persistence

Phase 1's catalog gains a second, UI-driven writer. Two operations:

- **Create** — the user uploads one garment photo in the browser. The backend
  runs the *same* local tagging pipeline (`05-vlm-tagging-spec.md`,
  Qwen3-VL-8B via llama.cpp) and returns the tagged result as a **draft that
  is not persisted**. The user reviews and may correct every tagging field,
  then saves; only then is the photo moved into `data/photos/` and the row
  written.
- **Edit** — every tagging field plus `notes` may be corrected on an existing
  item. `id`, `added_date`, and the photo are immutable in this feature; there
  is no re-tag and no photo replacement.

Why now: `05-vlm-tagging-spec.md` already expects "some manual correction
early on" (weaker subcategory accuracy, pattern catch-all). Correcting via
`cmd/ingest` means re-running inference on a photo when only a label is wrong;
the UI edit path corrects labels without touching the photo or the model.

Constraints unchanged: same base model, no fine-tuning, fully local, one photo
per garment, single embedded binary. The VLM call itself is unchanged — only
its trigger and the fact that a human confirms before persist.

**Not in scope:** delete, photo replacement/re-crop, re-tagging an existing
item, bulk import, multi-user/auth. The API surface is specified in
`07-architecture.md` "Catalog write API".

## Second catalog writer exists: extract shared persistence out of `cmd/ingest`

The earlier "Ingest→DB wiring: direct call, no provider/service abstraction
yet" entry deferred extraction until a second writer existed, naming a future
Gin route as the example. That condition is now met (the interactive
create/edit UI above), so the photo-copy + row-insert logic currently inline in
`cmd/ingest/main.go`'s `persist` moves to one shared internal package used by
both the CLI and the API. There is still exactly one persistence
implementation; the API route is a second *caller*, not a second copy.

Rejected: duplicating the logic in the handler (two copies that can drift); a
queue/service abstraction (no queue, no worker, one local process).

## Catalog writes must not leave orphan photos or dangling rows

Resolves the gap logged as `backlog/ING-028.md`. A create performs two writes —
the photo file and the catalog row — and must not leave a half-written result
when either fails:

- A failed create leaves **no orphan photo** in `data/photos/` and **no row**
  pointing at a missing photo.
- The failure is explicitly recoverable: re-running the create cannot leave
  duplicate state (the item id comes from the tagging pipeline, so a retry uses
  a fresh id and a failed attempt's residue is removed).

Implementation mechanism (which step commits first, staging location, cleanup
on each failure branch) is a ticket-level detail; the behavior above is the
contract. This closes ING-028's "behavior to be decided" and makes it
ticketable.

## Photo upload trust boundary: fixed types, size cap, server-derived filename

The upload endpoint is the first client-supplied file surface. Accepted
extensions are the same set the CLI already ingests — `.jpg`, `.jpeg`, `.png`,
`.webp` — and each upload is capped at 20 MiB. The stored name is always
server-derived (`<item_id>.<ext>`); the client-supplied filename is never used
as a path, so it cannot escape `data/photos/`. Serving already rejects
traversal via `validPhotoName` (`internal/api/api.go`). Staged uploads live
under a gitignored runtime staging directory and are removed after a successful
save or a failed create.

## Testing tooling: Vitest + Testing Library for the frontend, stdlib-first for Go

Current state: the Go suite is stdlib-only (`testing`, `-race`, `httptest`,
build-tagged process tests that boot the real binary) and is adequate — no
gap worth adding a framework for. The frontend has no JS test runner at all:
ING-020's UI acceptance criteria were verified *structurally*, by a
`node --test` suite reading `App.jsx` and the built bundle, not by rendering
React in a DOM. That is the actual gap this decision closes.

**Decision — frontend:** adopt **Vitest** with **jsdom** and
**@testing-library/react** (plus `@testing-library/jest-dom` matchers) as a
`frontend/` devDependency set.

- Vitest reuses the existing `vite.config.js` / ESM / JSX pipeline — no second
  transformer or bundler config to keep in sync with the app build.
- `environment: 'jsdom'` gives React a real DOM to mount in, so a test asserts
  rendered behavior (one card per item, placeholder on photo error, empty
  state) instead of grepping source strings.
- Testing Library queries by role/label/text, so assertions track what the
  user sees rather than component internals.
- fetch is stubbed with Vitest's built-in `vi.stubGlobal('fetch', …)`; no mock
  server dependency for the single `GET /api/items` endpoint.
- Frontend tests live under `frontend/` (co-located `*.test.jsx`, run via an
  `npm test` script), because the runner is frontend-scoped. This deliberately
  does not follow the Go `tests/` layout; each language's tooling owns its
  tests.
- Once Vitest covers the same branches, **retire
  `tests/frontend/ing_020_app_test.mjs`** — keeping both is duplicate
  maintenance.

**Decision — Go:** stay stdlib-first. No assertion framework is added.

- `testify` is rejected: the suite is intentionally stdlib-consistent, and
  swapping `t.Fatalf`-style table tests for it is churn, not coverage.
- `goleak` is rejected for now: the app spawns no long-lived goroutines of its
  own to leak.
- One additive test type needs no dependency: a stdlib fuzz target
  (`testing.F` / `go test -fuzz`) on `ParseTaggingResult` — a parser of
  untrusted model output is the textbook fuzz case.
- `govulncheck` is the one non-stdlib Go tool accepted, to scan the (large)
  Gin/pure-Go-SQLite indirect dependency tree for known CVEs.
- `golangci-lint` and a coverage gate are left for later; neither is needed to
  close a current gap.

**Hard constraint:** every addition above is a `devDependency` or a stdlib
tool — build/test-time only. Nothing ships in the embedded binary or runs at
runtime, so `00-overview.md`'s fully-local constraint and
`07-architecture.md`'s single-binary runtime model are untouched.

Rejected alternatives:
- **Jest** — a second transformer/config alongside Vite for no benefit over
  Vitest in a Vite project.
- **Playwright / Cypress (component or E2E)** — heavy for a single-user
  localhost app, and the Go integration tests already boot the real binary and
  exercise the embedded SPA + API end to end.
- **MSW** — overkill for one endpoint; add only if the API surface grows
  enough that hand-stubbing `fetch` becomes noisy.
- **happy-dom** — defaults to jsdom; revisit only if the suite is slow enough
  to matter.
- **Storybook** — no component library or design system to document.

Follow-ups (to be ticketed after this entry lands): the frontend tooling setup
(`vitest`/`jsdom`/Testing Library devDeps, `test` config, `npm test`), and the
retirement of the superseded `tests/frontend/ing_020_app_test.mjs`.

## Frontend embed placeholder: track `frontend/dist/index.html`, hide local builds with `skip-worktree`

`go:embed` fails to compile when its pattern matches no files, and the Vite
build output (`frontend/dist/`) is gitignored as generated output, so a fresh
checkout would have nothing for the embed to match. ING-021 therefore commits
a small placeholder at `frontend/dist/index.html` and keeps it tracked via a
`.gitignore` negation (`frontend/dist/*` plus `!frontend/dist/index.html` —
the negation cannot be written against a directory exclusion such as
`frontend/dist/`, because git will not re-include a file whose parent
directory is excluded).

The cost is that a real `npm run build` overwrites the tracked placeholder
with the built entry, which references content-hashed assets under
`frontend/dist/assets/` that stay gitignored. Committing that built
`index.html` would ship an entry pointing at assets missing from a fresh
checkout, so the overwrite must never be committed. Chosen handling: once the
placeholder is staged/committed, run
`git update-index --skip-worktree frontend/dist/index.html`. Git then ignores
the working-tree overwrite — the committed blob remains the placeholder —
while the working tree can hold the real built entry, so `go build` embeds and
serves the actual UI locally. Undo with
`git update-index --no-skip-worktree frontend/dist/index.html` when the
placeholder content itself needs to change. This is a local index flag only:
it changes nothing in the committed repository, and a fresh clone that runs
`npm run build` will see the placeholder appear modified unless it sets the
same flag.

Rejected alternatives:
- Track `frontend/dist/.gitkeep` with Vite `build.emptyOutDir: false`:
  permanently clean `git status`, but a fresh checkout then has no
  `index.html`, so `/` 404s instead of showing the "frontend not built"
  placeholder, and it still needs a non-default Vite setting.
- Commit the real built `index.html`: broken on a fresh checkout, since it
  references hashed assets that are not in git.
- Commit all of `frontend/dist/`: contradicts `07-architecture.md`'s
  "generated, gitignored" build output and puts build churn in history for no
  benefit.

## Ingest→DB wiring: direct call, no provider/service abstraction yet
`cmd/ingest` calls `internal/store` directly to persist a `Processor.Outcome`
as a catalog row — no intermediate service layer, queue, or provider
interface. `cmd/ingest` is currently the only writer, and abstracting for a
hypothetical second writer (e.g. a future Gin route that lets a user manually
re-tag or re-submit an item) buys nothing today and adds a layer with no
second implementation to validate it against.

Revisit when a second writer actually exists — extract the shared
persistence logic behind an interface at that point, not before. Until then,
`internal/store`'s exported functions are the only integration surface.

## Env var auto load
the .env vars are autoloaded using dotenv package. both in server and ingest

## Env var bounds: malformed VLM_URL passes through, unknown booleans error, negative delay clamps to 0
Settled the ING-009 audit of `VLM_*` env vars for the same "parseable
but semantically invalid" gap `VLM_TEMPERATURE` had (fixed in ING-008).

- **`VLM_URL`** — no URL-format validation at startup. A malformed value
  is accepted and surfaces on first use as the ING-002
  `ErrVLMUnreachable`, rather than a hand-rolled startup URL check.
  Rationale: that distinct, caller-handled error path already exists, so
  a second startup failure mode for URLs buys little.
- **`VLM_SERIALIZE_REQUESTS`** — strict `strconv.ParseBool`. An
  unrecognized string (`"yes"`, `"ture"`) is a startup error naming the
  variable, not a silent `false` — a typo'd boolean silently meaning
  "off" is exactly the class of bug this audit looked for.
- **`VLM_REQUEST_DELAY_MS`** — a negative value is clamped to `0` and
  logged as a startup warning; it is neither stored as-is nor a hard
  error. "Wait a negative time" is meaningless, but a typo shouldn't
  block startup — warn so it's visible.
- **`VLM_API_KEY`** — opaque string, no client-side format check; empty
  means no Authorization header. API keys have no checkable client-side
  format; validity is determined by the VLM server's response.

## VLM_TEMPERATURE acceptable range: 0.0–1.0, default 0.4
Bounded below the completion API's technically-wider range (commonly
0–2) because this project's use is structured JSON tagging, not
open-ended generation — pushing temperature much past 1.0 mainly
inflates the malformed-JSON rate that ING-005's retry policy exists
to handle, not the useful answer-diversity it's meant to produce. 0.0
stays valid (not banned) for deliberate deterministic runs, but isn't
the default, since a 0.0 retry mostly just reproduces attempt 1's
answer rather than giving a genuinely independent second sample.
Enforced at startup (ING-006) — out-of-range same as non-numeric: a
startup error naming the valid range.

## Malformed VLM output: retry once at nonzero temperature, then flag
`05-vlm-tagging-spec.md` originally left "reject and retry" vs. "flag
for manual review" as an either/or, unresolved. Settled as: retry
exactly once, then flag if the retry also fails.

VLM sampling temperature is set to a nonzero default (`VLM_TEMPERATURE`,
default `0.4` — `06-decisions.md`'s usual "reasonable default, tune
later with evidence" pattern, not a firm number) specifically so the
retry is a genuinely independent second sample of the model, not a
near-deterministic repeat of the first answer. A retry at temperature
0 would mostly just reproduce the same output, defeating the point.

Rejected: no retry (too much manual-review noise for what's often a
one-off glitch) and retry N>1 times (added complexity and GPU load
with no evidence yet that more than one retry helps — see below).

Every attempt is logged in full (raw response, parsed JSON if any,
specific failure type and detail) to `logs/vlm-attempts.jsonl`, keyed
by a shared `item_id` per photo so attempt 1 and attempt 2 can be
joined. This was deliberately structured to double as input for the
VLM calibration idea parked in `later-ideas.md` — comparing two
independent samples of the same photo is exactly that idea's
"wrong vs. inconsistent" signal, now collected automatically during
real use instead of needing a separate offline harness. If real
flagged-log data later shows retries rarely change the outcome, or
shows a second retry would help, that's the evidence to revisit this
policy — not a reason to guess further now.

VLM-unreachable errors (ING-002) are explicitly excluded from this
policy — a connectivity failure is not a malformed-output case and
should surface immediately rather than being retried into a flag.

See `backlog/ING-005.md`, `backlog/ING-006.md`, `backlog/ING-007.md`.

## Bash permission checks are per-sub-command, not per-raw-string
Investigated after observing that chained bash commands (e.g. `echo
"x" && go vet ./... && go test ./...`) consistently triggered an "ask"
prompt whenever any single piece lacked a matching permission rule,
with the approval dialog listing each piece separately.

Confirmed against opencode 1.18.30's actual source
(`packages/opencode/src/tool/shell.ts`, registered under the `bash`
tool ID): commands are parsed with a real bash-grammar parser, and
every distinct sub-command the parser identifies (correctly split
across `&&`, `||`, `;`, `|`) is checked against permission rules as
its own independent resource. Chaining does not let an unapproved
command hide behind an approved one, and a wildcard rule like `"git
diff*"` cannot match past an operator into an unrelated appended
command — each side of the chain is evaluated on its own.

A separate, unrelated file in the same codebase
(`packages/core/src/tool/bash.ts`, a "minimal V2 core" scaffold
explicitly marked as not yet having this parsing ported in) does treat
the whole raw command string as a single resource, and matches the
behavior described in an open opencode GitHub issue. That file is not
imported anywhere in the running `opencode` package as of this
version — it does not affect actual behavior. Worth re-checking if
opencode's "V2" migration referenced in that file's TODOs ever lands
and replaces the current shell tool.

Practical takeaway logged in `AGENTS.md`: the earlier "never chain
bash commands" instruction was written on an incorrect security
assumption (that chaining could bypass a permission boundary) and has
been corrected to a workflow preference instead — chaining is safe,
but a single unapproved piece still blocks the whole call, so separate
calls stay easier to review and fail more predictably. The stronger
fix for prompt fatigue is `AGENTS.md`'s existing guidance to prefer
dedicated tools (`list`, `grep`, `read`) over their bash equivalents,
since those are separate permission categories already set to `allow`
and never touch the bash arity/parsing path at all.

## Distribution model: self-hosted single-user, not multi-tenant
"Fully local" means each install runs entirely on infrastructure its
owner controls — not that the project is single-person-only. Other
people may run their own instance (own binary, own SQLite file, own VLM
endpoint) exactly as Ali does. This changes nothing about the current
architecture: one binary, one SQLite file per install, no auth, no
`user_id` on any table — that model was already implicitly
"distributable," it just hadn't been stated as intent. A real
multi-tenant deployment (shared server, multiple accounts, auth, one
DB serving many users) is a distinct future direction — see
`later-ideas.md` — and is explicitly not what "other people may use it"
means today.

## Weather signal is an acknowledged exception to "fully local"
The fully-local hard constraint (`00-overview.md`) governs inference and
storage — no cloud AI, no hosted model APIs, no data leaving the user's
own hardware for tagging or cataloging. Weather forecast data has no
local source by nature, so Layer 2 (`00-overview.md` Vision) will call
an external weather API when it's built. This is a scoped, single
exception, not a loosening of the constraint elsewhere — VLM inference,
the LLM recommender, and the catalog store stay fully local regardless
of distribution model.

## `LLM_URL`/`LLM_API_KEY` provisioned ahead of use
Phase 1 ingestion only calls `VLM_URL` (image tagging). `LLM_URL`/
`LLM_API_KEY` are defined in the env contract now but have no consumer
yet — they're provisioned for Phase 3 (outfit recommender), which will
need a text-only LLM to reason over the tagged catalog + weather signal
and pick outfits. Both still follow the standard contract (`LLM_URL`
errors at startup if empty when eventually wired up; `LLM_API_KEY`
optional) — but until a Phase 3 ticket actually calls it, it's inert
config, not a bug or an oversight. See `07-architecture.md` for the full
env var contract.

## VLM request concurrency: concurrent by default, serialize as opt-in workaround
Default backend behavior is **concurrent** requests to the VLM — no
artificial single-request bottleneck, so the app doesn't stall waiting
on a queue when there's no known problem with the model in use.

For VLMs with a known parallel-request issue (llama.cpp GitHub issue
`#17200` — KV cache corruption on the second multimodal request in the
same server session, reported against Qwen3-VL, no confirmed fix as of
review) an opt-in env var (`VLM_SERIALIZE_REQUESTS`) forces the backend
into a global one-at-a-time queue/mutex around VLM calls. A second,
independent env var (`VLM_REQUEST_DELAY_MS`) adds a post-response delay
on top of serialization as a further dodge. The delay is only
meaningful when serialization is on; if it's set to a nonzero value
while serialization is off, the backend logs a startup **warning**
(not a hard error) and no delay is applied.

Once the upstream issue is confirmed fixed, or a VLM without the
problem is in use, both vars stay at their defaults (off) and no code
change is needed. Full env var contract in `07-architecture.md`.

## Stack: Go (Gin/GORM/SQLite) backend, React (Vite/Tailwind v4) frontend, single embedded binary
Backend is Go with Gin for routing and GORM (SQLite driver) for
persistence — matches existing Go proficiency, no framework overhead
beyond what this surface area needs. Frontend is a React SPA built with
Vite and styled with Tailwind v4, not Next.js — Next's SSR/routing/API-route
features solve problems this project doesn't have (no multi-user,
no deployment, no SEO), and would mean running a second (Node) server
process alongside Go for no benefit. The Vite build output is embedded
into the Go binary via `go:embed`, so the whole app — API and UI — ships
and runs as a single executable, served over `localhost` and opened in
a browser tab (not wrapped as a native desktop app). Full architecture
notes in `07-architecture.md`.

## Sprint branches, one branch per sprint
Each sprint (`02-agile-process.md`'s definition — one coherent pipeline
slice) gets its own branch off `main`, named `sprint/<slug>`. Ali creates
and merges sprint branches; coding agents commit to the current sprint
branch but never create, push, or merge branches themselves — branch
lifecycle is an infra decision, which stays with Ali per the roles split
in `00-overview.md`. Enforced via each agent's `permissions` block in
`.opencode/agents/` (git branch/push/merge commands denied outright).

## Backlog stored as one file per ticket under `backlog/`
Ticket status (board column) is tracked with a `**Status:**` line inside
the ticket file rather than separate per-column directories. Chosen so
each agent's filesystem permission is a single `backlog/**` glob instead
of a moving target across directories — simpler to reason about and to
encode in `.opencode/agents/*.md` permission blocks. See
`02-agile-process.md` Backlog storage section.

## Scratch/test files confined to `<project root>/temp/`, ask-gated
Coder and Tester occasionally need scratch space during implementation or
test runs. Agents may not write outside the project root without explicit
ask-approval, and even inside the project the only writable scratch
location is `temp/` (should be added to `.gitignore`). This replaces an
earlier pattern where an agent used the OS `/tmp` directory, which broke
reproducibility and left files outside version control and outside the
project sandbox entirely. Enforced via each agent's `permissions` block
in `.opencode/agents/` (`temp/**` is ask-gated, `/tmp/**` and
`external_directory` are denied outright).

## No `season` field in phase 1 schema
`warmth_tier` (light/medium/heavy) stays as the only thermal/seasonal
signal on the wardrobe item for now. `category` + `subcategory` +
`warmth_tier` already carry most of the seasonal signal, and the VLM
can't reliably read fabric/weave from a single photo — the one thing
that would actually distinguish season from warmth (e.g. a linen shirt
vs. a cotton tee at the same warmth tier). Adding a VLM-tagged `season`
field now would mean a new low-confidence enum with no clear payoff,
since its consumer — the recommender — is Phase 3 and not being built
yet. If needed later, derive it from fields already captured here (rule
lookup or recommender logic) rather than reopening the tagging spec.
Closes the open question that was in `04-data-schema.md`.

## Fully local, no cloud hosting
The whole system (model inference, catalog store, UI) runs on Ali's own
hardware. No cloud services involved at any layer.

## No model fine-tuning
A base VLM with constrained, taxonomy-anchored prompting covers the
accuracy need for a personal-scale catalog. Fine-tuning would add real
effort (dataset curation, training loop, evaluation) for a gain that isn't
needed by the product goal. Revisit only if the base model proves
*systematically* wrong on a class of items after real use — not before.

## Qwen3-VL-8B as the tagging model
Apache 2.0, ~6GB at Q4 quantization — comfortable fit on a 12GB GPU
alongside other local inference workloads run occasionally rather than
concurrently. Chosen over Qwen2.5-VL (older generation) and Gemma 3 4B
(easier setup, weaker vision benchmarks).

## One photo per garment (flat lay/hanger), not outfit photos
Removes the need for multi-item detection, occlusion handling, or a
worn-vs-flat router entirely — single dominant subject per photo, much
simpler ingestion pipeline. Outfit-photo cataloging is explicitly
out of scope, not a future phase commitment.

## Phase 1 scope = ingestion pipeline only
Weather integration and the recommender are deferred until the catalog is
built and used for a couple of weeks — validates the schema and tagging
accuracy against real use before more is built on top of it.
