# 0001. Advisor guidance is soft prompt text, not validation

- **Status:** accepted by Ali, 2026-10-04. Needs the `07-architecture.md`
  edit proposed in `backlog/ING-060.md` before ING-060 can be built.
- **Supports:** `07-architecture.md` "Recommender (Phase 3)" step 4;
  `06-decisions.md` "Recommender approach". This record never overrides them.

## Context

The recommender's system prompt is mostly the output contract. Its only
styling advice is one line ("pair neutrals with one accent, avoid clashing
colors and too many patterns"). The LLM is in the loop precisely because
hand-written color rules are brittle (`06-decisions.md` "Recommender
approach"), but it gets almost nothing to reason with. Most LLMs do not
behave like a stylist unprompted, and small ones cannot reliably map
abstract color theory onto tags like `dominant_color=olive pattern=plaid`.

The recommender must work with almost any OpenAI-compatible endpoint, from a
small local model to a hosted one.

## Decision

1. **Soft.** Styling advice goes in the prompt only. `validate()` and the
   output contract do not change. A rule is promoted to a hard check only
   with eval evidence (ADR 0002) and a spec change.
2. **Concrete rules with a short why.** Every rule is written in taxonomy
   values (`03-taxonomy.md`), so the model can apply it to the tags it sees.
   Each rule block has a one-line reason so the model can handle
   combinations the rules do not list.
3. **Topics:** color, pattern, layering, formality cohesion, accessories and
   headwear, weather sense beyond warmth, variety across the 3 outfits. Fit,
   silhouette, fabric and trends are out: there is no data for them.
4. **Reason:** stays one sentence, but must name the color or pattern logic
   and how the outfit fits the weather or the note.
5. **Portable:** plain system and user messages. No JSON mode, no reliance on
   a model's thinking mode, no scratchpad field in the output.
6. **Weather in words:** the prompt labels the WMO condition code in words
   (a Go copy of the frontend's table) and states whether rain is likely
   (the existing `RainHint`).
7. **Where it lives:** an embedded `internal/recommend/advisor_prompt.md`
   (`//go:embed`). The output contract stays a Go constant, so a style edit
   cannot break validation.
8. **Generic wearer:** no information about the person. Preferences wait for
   per-user settings (`later-ideas.md` "Multi-user / multi-tenant support").

Style rulings, where advice disagrees:

| # | Point | Ruling |
|---|---|---|
| 1 | Neutrals | `black`, `white`, `gray`, `navy`, `tan`, `beige`, `brown`; `blue` on `jeans` is neutral; `olive` is near-neutral |
| 2 | Accents per outfit | at most one accent color; shades of one color count as one |
| 3 | `black` + `navy` | allowed |
| 4 | `black` + `brown` | allowed in casual; avoid in formal outfits |
| 5 | Tonal / monochrome | allowed, at most one of the 3 outfits |
| 6 | Patterns per outfit | at most one patterned garment; a patterned accessory only when every garment is `solid` |
| 7 | Belt vs. shoes | belt matches the shoe color family (`brown`/`tan` together, `black` with `black`) |
| 8 | `white` `sneakers` | a neutral that goes with any casual or smart-casual outfit |

## Rejected

- **Hard validation of style rules:** contradicts the reason the LLM is
  there, needs two protected spec changes, and turns a mediocre outfit into a
  `502` on a small or filtered wardrobe.
- **Abstract color theory only:** small models misapply it to taxonomy color
  names.
- **Scratchpad field (`"analysis"`) or native thinking mode:** changes the
  JSON contract or depends on the server; not portable.
- **Prompt file loaded at runtime:** a new env var, a new startup failure, and
  a prompt that can drift from the tested one.
- **Wearer profile (static or in the UI):** out of scope until per-user
  settings exist.

## Consequences

- Ali edits `07-architecture.md` in two places (wording in
  `backlog/ING-060.md`).
- The WMO label table exists twice (frontend and `internal/recommend`). It is
  a fixed standard, so drift is unlikely.
- The system prompt grows from about 10 lines to about 60–100.
- Guidance can still be ignored. ADR 0002's eval measures how often.
- The recommender sends tags, weather and the note to whatever `LLM_URL`
  points at. Ali currently uses a hosted endpoint, which `06-decisions.md`
  "Fully local" does not yet record.
