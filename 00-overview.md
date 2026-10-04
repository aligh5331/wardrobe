# Wardrobe — project overview

> Rename this file's H1 once you pick a real project name.

## Vision
A fully local personal wardrobe system, built in three layers:

1. **Catalog** — photo of a garment in → categorized, color-tagged item out
2. **Weather signal** — forecast data for a given day
3. **Recommender** — picks weather-appropriate, color-coordinated outfits from the catalog

## Current phase
**Between phases: Phase 3 closed, Phase 4 (the outfit log) being specified.** Nothing in Phase 4 is ticketable until its scope is in `06-decisions.md` and the specs. Phase 1 (catalog), Phase 2 (weather signal) and Phase 3 are built and done. Phase 3 added Layer 3: on request, the backend filters the catalog by today's weather and an optional formality, and the local text LLM (`LLM_URL`) picks three color-coordinated outfits from those candidates (`07-architecture.md` "Recommender"). Embeddings and outfit image generation stay parked in `later-ideas.md`.

## Roles
- **Ali (human, orchestrator)** — captures garment photos, makes judgment calls on ambiguous items, reviews all agent output, owns infra/tooling decisions. Not writing implementation code directly.
- **Claude, this project** — design and spec-writing **partner**. Helps turn ideas into specs, backlog items, and docs that a coding agent can execute. Does not implement code itself in this chat.
- **Coding agents** (run via Ali's own agent harness / Claude Code, outside this project) — execute backlog tickets against the specs produced here. See `01-agentic-workflow.md` for the agent roles and `agents/*.md` for each role's definition.

## Hard constraints (do not relitigate — see `06-decisions.md` for the full log)
- Fully local. No cloud services, no hosted inference APIs, nothing trained in the cloud. Two scoped exceptions (`06-decisions.md`): weather data from Open-Meteo, and the recommender LLM, which may be a hosted endpoint by the owner's choice of `LLM_URL`.
- No model fine-tuning. Use a base local VLM (Qwen3-VL-8B) with constrained prompting against `03-taxonomy.md`, and a base local text LLM for recommendations.
- One photo per garment (flat lay/hanger) — not outfit photos on a person.

## File map
| File | Purpose |
|---|---|
| `00-overview.md` | This file |
| `01-agentic-workflow.md` | The dev-process agent roles and how work flows between them |
| `02-agile-process.md` | Backlog format, board, definition of ready/done |
| `03-taxonomy.md` | Fixed category list + color palette — source of truth for tagging |
| `04-data-schema.md` | Wardrobe item schema |
| `05-vlm-tagging-spec.md` | Ingestion contract: prompt + model + output shape |
| `06-decisions.md` | ADR-style decision log |
| `07-architecture.md` | Stack, runtime model, and env var contract |
| `agents/tester.md` | Example agent role definition |
