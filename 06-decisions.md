# Decisions

Newest first. Each entry: decision, date-ish context, why.

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
