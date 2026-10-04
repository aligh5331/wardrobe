# 0002. Recommender quality is measured by a human-rated eval form

- **Status:** accepted by Ali, 2026-10-04. Built by `backlog/ING-059.md`.
- **Supports:** `07-architecture.md` "Recommender (Phase 3)";
  `06-decisions.md` "Testing tooling" (stdlib-first for Go). This record never
  overrides them.

## Context

The recommender had never been stress-tested, so there was no baseline.
ADR 0001 makes the advisor guidance soft, so tests cannot assert taste, and
LLM output varies from run to run. "Better" needs a number to compare, and
taste needs a human.

## Decision

1. **Opt-in live eval.** A `//go:build integration` test, like
   `tests/ing_004_live_test.go`. It loads `<repo root>/.env` and skips when
   `LLM_URL` is unset, so CI never calls an LLM. It runs the real
   `RulesFor`, `Filter`, `MissingSlots` and `Picker.Pick`.
2. **Trap wardrobe × 4 scenarios × 3 runs.** A fixed synthetic catalog with
   deliberate temptations. Scenarios: `cold-rain`, `mild-dry` (with a note),
   `hot-sunny`, `temp-unknown`.
3. **Validity is the only hard check.** A run that `Pick` rejects fails the
   test. Auto-flags count guidance violations and are reported only.
4. **Human in the loop via Markdown.** Each run writes a form Ali fills in.
   It is "light": per outfit a score 1–5, would wear y/n and a note, and per
   scenario a variety score 1–5. Ali rates the first valid run of each
   scenario, about 12 outfits.
5. **Forms are tracked.** Files go in `tests/evals/recommender/`. They hold
   synthetic items and the model name only, never URLs, keys or personal data
   (`AGENTS.md` section 9). Each form records the commit and a prompt
   fingerprint.
6. **Summary command.** A second integration test reads the filled forms
   (no LLM call) and prints one comparison row per form.
7. **Baseline first.** The eval lands and runs on the current prompt
   (ING-059) before the prompt changes (ING-060).

## Rejected

- **Unit tests only:** they prove nothing about outfit quality.
- **Failing on auto-flags:** contradicts soft guidance and would be flaky.
- **Detailed per-topic ratings:** about 250 ratings per eval, which nobody
  keeps filling in.
- **Forms in `temp/`:** gitignored, so the baseline would be lost.

## Consequences

- One eval costs 12 LLM calls (up to 24 with retries). With the 120 s
  timeout it needs `go test -timeout 30m`.
- The trap wardrobe and scenarios are part of the fingerprint. Changing them
  starts a new comparison series.
- Auto-flags cover a subset of the guidance. Taste beyond them is judged in
  the form.
