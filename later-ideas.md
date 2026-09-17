# Later ideas (not approved specs)

Ideas parked for future consideration. Nothing in this file is ticketable
by Planner or actionable by Coder until it's promoted into an approved
spec (`03-taxonomy.md` / `04-data-schema.md` / `05-vlm-tagging-spec.md`)
via the normal change process. Deliberately **not** numbered into the
`0*.md` sequence so it's never mistaken for one of those.

---

## Outfit log and feedback loop

### Problem

There's currently no way to record that an outfit was actually worn, or
whether it worked. Without that, "did this suggestion work" is
unanswerable — any future recommender (rule-based, retrieval, or
learned) has nothing to learn from, since no history exists to learn
from in the first place.

### Idea

Add an `outfit_worn` record, logged manually by Ali after wearing an
outfit (not something the recommender infers on its own):

```
outfit_worn: { id, date, item_ids[], weather_snapshot, rating, notes }
```

Start with the simplest possible consumer of this data — no ML:

- Per-item and per-category-pair aggregate stats (e.g. "navy sweater +
  gray chinos: 4.3 avg rating, worn 3x")
- Surface these stats back to Ali as a sanity check before any
  recommender logic is built on top of them

This is deliberately separable from the garment-embeddings idea below —
the log is useful on its own (a personal outfit history/journal) even
if embeddings are never built, and co-occurrence stats give a signal
of whether there's enough data density to justify anything fancier
later.

### Explicitly out of scope for now

- Not a Phase 1 or Phase 2 concern — this is Phase 3 (recommender)
  territory, and the recommender itself doesn't exist yet.
- No automatic outfit detection from photos — ratings are entered by
  Ali against a set of `item_id`s he picks, not inferred by AI.
- No learned/trained scoring model at this stage — co-occurrence stats
  only. A trained compatibility model is a separate, later idea (see
  below) with its own open question about the no-fine-tuning
  constraint.

### Open threads this could eventually feed

- Garment embeddings (below) — a RAG-style recommender would retrieve
  highly-rated past `outfit_worn` records as few-shot examples for the
  Phase 3 LLM, once both this log and embeddings exist.

---

## Garment embeddings for visual similarity / compatibility

### Problem

The categorical schema (`04-data-schema.md`) is enough for rule-based
filtering (correct warmth tier, not-clashing colors by enum) but can't
express two things Ali actually cares about:

- Visual similarity *within* a category/color that the enum flattens
  (two "navy sweaters" can look nothing alike)
- Fuzzy compatibility judgments ("these two happen to go well
  together") that don't reduce to hand-written color/formality rules

Note: this only addresses *visual* compatibility from the photo. It
cannot capture "this shirt feels good on me" — fit/fabric/comfort isn't
visible in a flat-lay photo. If that signal matters, it has to come in
as an explicit rating on the item itself (see the outfit-log idea
above), not something inferred from an embedding.

### Idea

- Compute an embedding per garment photo at ingestion time (alongside
  the existing VLM tagging call, not a new user-facing step), using a
  local CLIP or fashion-tuned CLIP variant (e.g. FashionCLIP) — small
  model, comfortable on the 3060 alongside/after the VLM pass
- Store as a new column or `garment_embeddings` table keyed by
  `item_id`
- Storage/search via `sqlite-vec` rather than a standalone vector DB —
  keeps the single-embedded-binary architecture (`07-architecture.md`)
  intact, and brute-force cosine over a few hundred items doesn't need
  a real index anyway
- Consumer: nearest-neighbor "find items similar to this one," and/or
  as a feature feeding a future compatibility step in the Phase 3
  recommender (e.g. retrieval into the LLM's prompt rather than a
  trained scorer — see open question below)

### Explicitly out of scope for now

- Not a Phase 1 concern — this is additive to the Phase 3 recommender,
  not the ingestion pipeline.
- Does not replace the categorical schema — embeddings are a secondary
  layer on top of, not instead of, `category`/`color`/`warmth_tier`
  filtering.
- No trained compatibility model yet. A small classifier/scorer trained
  on logged outfit ratings (embeddings-in, rating-out) is a plausible
  future step but raises an open question worth resolving explicitly
  when it comes up: the hard constraint says "no fine-tuning" in the
  context of the tagging VLM specifically, but training any model on
  Ali's own data arguably sits in the same spirit and shouldn't be
  assumed fine just because it's a different model. Flag to Ali before
  building this, don't default into it.

### Open threads this could eventually feed

- Outfit log and feedback loop (above) — retrieval of highly-rated past
  outfits by embedding similarity is the natural bridge between the two
  ideas.
- VLM calibration dataset idea (already in this file) — a fashion-tuned
  embedding model could also be evaluated as part of that same scraped
  sample set.

---

## VLM tagging calibration via scraped catalog images

### Problem

Taxonomy gaps currently surface one real item at a time during manual
testing (e.g. the metallic-color gap found from the watch test — `gold`
and `silver` returned outside the 16-color palette, `06-decisions.md`).
That's slow, unsystematic, and gives no signal on how *consistent* the
VLM is on a given item type — only whether one single guess happened to
be right or wrong.

### Idea

- Build a sample image set by pulling garment photos from online
  retailers with good-quality studio product photography — single item
  per photo, which already matches the "one photo per garment" ingestion
  assumption reasonably well.
- Run the current VLM + prompt (`05-vlm-tagging-spec.md`) against the
  sample set for `category`, `subcategory`, `dominant_color`,
  `secondary_colors`, `pattern`.
- Run the **same** photo through multiple times and compare outputs:
    - A *wrong* answer (e.g. calling a hoodie a sweater) is a
      taxonomy/prompt gap.
    - An *inconsistent* answer across repeated runs on the identical photo
      is a different problem — a reliability signal for that item type,
      independent of whether any single run happened to be "correct."
- Aggregate results to see: which values fall outside the current
  taxonomy enums (the gold/silver case), which subcategories get
  confused with each other, and which categories have the highest
  run-to-run variance.
- Use that aggregate picture to make one informed taxonomy-extension
  decision (e.g. adding metallic color values, or not) instead of
  patching the palette ad hoc every time a new edge case shows up.

### Explicitly out of scope for now

- Not a Sprint 0/1 blocker. Current `03-taxonomy.md` / `04-data-schema.md`
  stay exactly as they are until this is actually run and reviewed.
- This is evaluation/calibration, not training — still respects the
  no-fine-tuning hard constraint (`00-overview.md`). It's structured
  prompt/taxonomy testing at scale, not a training loop.
- Sourcing photos from online retailers is fine for internal, one-time
  calibration use, but isn't "Ali's own wardrobe" data — flag this again
  if the sample set or its outputs were ever repurposed beyond internal
  testing.
- If/when picked up, this likely lives as a one-off analysis script
  (maybe under a `tools/` or `experiments/` dir, TBD), not as part of the
  production ingestion pipeline — worth its own mini-spec before any
  agent touches it.

### Open threads this could eventually help resolve

- Metallic color taxonomy gap (gold/silver, from the watch test) —
  `06-decisions.md`
- General subcategory accuracy — already flagged as "weaker than category
  accuracy" in `05-vlm-tagging-spec.md`'s known limitations section

## Multi-user / multi-tenant support

### Direction

"Other people may use it" currently means self-hosting: each person
runs their own instance (own binary, own SQLite file, own VLM endpoint),
identical to Ali's setup. A real multi-tenant deployment — one shared
server, multiple user accounts, one DB instance serving many people
simultaneously — is a distinct, larger direction flagged as good but
not current scope.

### What it would actually change (not a small add-on)

- **Schema:** every table (`04-data-schema.md`'s wardrobe item and
  anything added later) would need a `user_id` / tenant column, plus
  whatever indexes/constraints follow from that.
- **Auth:** currently none — single local user, no login. Multi-tenant
  needs real auth (sessions, accounts, password/OAuth, something), which
  is a meaningfully different security surface than "trusted localhost
  app."
- **Runtime model:** `07-architecture.md`'s single-embedded-binary,
  single-SQLite-file, localhost-only model would need rethinking —
  concurrent access from multiple real users, probably a real deployment
  target instead of "open a browser tab on your own machine."
- **VLM/LLM connectivity:** `VLM_URL`/`LLM_URL` are currently one
  endpoint per install. Multi-tenant either means every user configures
  their own model endpoint (more UI/config surface) or a shared
  inference backend serving multiple users' tagging requests (capacity
  planning, queueing, isolation between users' data mid-request).
- **Hard constraints stay:** "fully local" and "no fine-tuning" don't
  change in spirit — this is about who's *running* an instance and how
  many accounts one instance serves, not about routing inference to the
  cloud. A multi-tenant instance is still one person's (or one
  household's, or one self-hoster's) own infrastructure serving multiple
  accounts on it, not a hosted SaaS.

### Explicitly out of scope for now

- Not a Phase 1 concern at all — Phase 1 finishes as single-user,
  self-hosted, exactly as currently speced.
- Not a small schema patch — this is closer to "a second product mode"
  than an incremental feature, and deserves its own spec pass (probably
  its own `0X-multi-tenancy.md`) if/when it's picked up, not a quiet
  retrofit onto the Phase 1 schema.
