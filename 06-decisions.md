# Decisions

Newest first. Each entry: decision, date-ish context, why.

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
