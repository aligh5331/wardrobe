# Wardrobe — project overview

> Rename this file's H1 once you pick a real project name.

## Vision
A fully local personal wardrobe system, built in three layers:

1. **Catalog** — photo of a garment in → categorized, color-tagged item out
2. **Weather signal** — forecast data for a given day
3. **Recommender** — picks weather-appropriate, color-coordinated outfits from the catalog

## Current phase
**Phase 1 only: the ingestion/cataloging pipeline.** Layers 2 and 3 are out of scope until phase 1 is built, used for a couple of weeks, and proven accurate enough on the real wardrobe.

## Roles
- **Ali (human, orchestrator)** — captures garment photos, makes judgment calls on ambiguous items, reviews all agent output, owns infra/tooling decisions. Not writing implementation code directly.
- **Claude, this project** — design and spec-writing **partner**. Helps turn ideas into specs, backlog items, and docs that a coding agent can execute. Does not implement code itself in this chat.
- **Coding agents** (run via Ali's own agent harness / Claude Code, outside this project) — execute backlog tickets against the specs produced here. See `01-agentic-workflow.md` for the agent roles and `agents/*.md` for each role's definition.

## Hard constraints (do not relitigate — see `06-decisions.md` for the full log)
- Fully local. No cloud services, no hosted inference APIs, nothing trained in the cloud.
- No model fine-tuning. Use a base local VLM (Qwen3-VL-8B) with constrained prompting against `03-taxonomy.md`.
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
