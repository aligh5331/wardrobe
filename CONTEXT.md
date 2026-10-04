# Context

Glossary of domain terms. Supporting material only: the numbered specs
`00`–`07` and `06-decisions.md` are authoritative (`AGENTS.md` section 2), and
taxonomy values come only from `03-taxonomy.md`.

## Catalog

**Item**: one garment row in the catalog (`04-data-schema.md`). One photo
per item.
_Avoid_: piece, product, clothing (as a noun for one row).

**Tags**: an item's taxonomy fields: `category`, `subcategory`,
`dominant_color`, `secondary_colors`, `pattern`, `warmth_tier`, `formality`.
The recommender sees tags only, never photos or `notes`.

## Recommender

**Candidate**: an item that survives the rule filter for today's weather and
the chosen formality (`06-decisions.md` "Recommender approach"). Only
candidates reach the LLM.

**Weather rules**: the warmth filter (allowed warmth tiers for top, bottom,
outerwear, footwear) plus the outerwear rule, both derived from today's
forecast. **Ignoring weather** switches both off for one request: no warmth
filter, outerwear optional.
_Avoid_: "weather filter" for just one of the two; "rules" alone.

**Missing slot**: a required slot with no candidate. Each one is explained by
its cause: none owned, excluded by warmth, excluded by formality. An item
failing both filters counts under both.

**Slot**: the role an item's category plays in an outfit. `top`, `bottom`,
`footwear` are required; `outerwear` follows the outerwear rule; `headwear`
(at most one) and `accessory` are optional.

**Outfit**: a set of candidate ids that fills the slots, plus a one-sentence
**reason**. A recommendation is exactly 3 outfits.

**Outerwear rule**: `required`, `optional` or `excluded`, from the
feels-like band (`06-decisions.md` "Weather → warmth thresholds").

**Rain hint**: `precipitation_probability_max >= 50`. Guidance for the LLM,
never a filter. Stated in the prompt as "rain likely today: yes / no".

**Output contract**: the JSON shape and the slot, outerwear, headwear and id
rules that `validate()` enforces (`06-decisions.md` "Recommender output
validation"). Breaking it makes the attempt invalid.

**Advisor guidance**: the soft styling rules the LLM is asked to follow
(color, pattern, layering, formality, accessories and headwear, weather,
variety, reason). Lives in `internal/recommend/advisor_prompt.md`. Never
validated; see `docs/adr/0001-advisor-guidance-is-soft-prompt-text.md`.
_Avoid_: "rules" alone, which is ambiguous with the output contract and the
warmth rules.

**Neutral**: `black`, `white`, `gray`, `navy`, `tan`, `beige`, `brown`, plus
`blue` when the item is `jeans`. Pairs with anything.

**Near-neutral**: `olive`. Behaves like a neutral.

**Accent**: any other palette color (`red`, `green`, `burgundy`, `pink`,
`purple`, `yellow`, `orange`, and non-jeans `blue`). At most one accent color
per outfit; shades of one color count as one.

## Recommender eval

**Eval**: the opt-in live run of the real recommender against the configured
`LLM_URL` (`docs/adr/0002-recommender-eval-is-human-rated.md`).

**Trap wardrobe**: the eval's fixed synthetic catalog. It deliberately
contains temptations (two patterned tops, loud accents, a beanie, sandals,
dress shoes next to shorts, black and brown belts) so weak guidance shows.

**Scenario**: one named weather input (plus optional note) the eval runs,
e.g. `cold-rain`, `hot-sunny`.

**Auto-flag**: a deterministic check that counts one kind of advisor
guidance violation in eval output. Reported, never a test failure.

**Eval form**: the Markdown file an eval run writes to
`tests/evals/recommender/`, which Ali fills in (score 1–5, would wear y/n,
note, variety per scenario).

**Prompt fingerprint**: a short hash of the exact prompt messages the eval
sent. It tells forms from different prompts (or wardrobes, or scenarios)
apart.

**Baseline**: the eval form from the prompt before a change; the reference a
later form is compared against.
