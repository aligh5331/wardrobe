# Data schema — wardrobe item

One row per garment. All enum fields validate against `03-taxonomy.md`.

| Field | Type | Notes |
|---|---|---|
| `id` | string (uuid) | primary key |
| `category` | enum | from taxonomy |
| `subcategory` | enum, required | from taxonomy, must be valid for the item's `category` |
| `dominant_color` | enum | from taxonomy palette |
| `secondary_colors` | list[enum] | 0 or more, from taxonomy palette |
| `pattern` | enum | solid / striped / plaid / print, from taxonomy |
| `warmth_tier` | enum | light / medium / heavy |
| `formality` | enum | casual / smart-casual / formal |
| `photo_path` | string | path to the stored garment photo |
| `added_date` | date | when cataloged |
| `notes` | string, optional | free text, not used by the recommender |

## Write-path rules (interactive create/edit)

- `id` is generated once at create and is immutable.
- `added_date` is set once at create and is immutable on edit.
- `notes` is optional and editable.
- All other fields are editable on an existing item; the photo and
  `photo_path` are not replaced in Phase 1 (`06-decisions.md`).

## Resolved
- `subcategory` is **required** — every tagging result must include a
  valid subcategory for its category, no null.
- `pattern` is in scope for phase 1 (added above) — needed to represent
  multi-color garments (stripes, plaid) that don't fit a simple
  dominant/secondary color model.
- No `season` field in phase 1. `category` + `subcategory` +
  `warmth_tier` already carry most of the seasonal signal (e.g. shorts
  vs. sweatpants, light vs. heavy), and the VLM can't reliably see the
  fabric/weave detail (`05-vlm-tagging-spec.md`) that would actually
  distinguish "linen, strictly summer" from "cotton, wears year-round"
  at the same warmth tier — a tagged `season` field would just be a
  low-confidence guess with no consumer yet (the recommender that would
  use it is Phase 3, not being built now). If a season signal turns out
  to be needed, derive it later — rule lookup or recommender logic —
  from fields already captured here, rather than adding a new VLM-tagged
  column. Full reasoning in `06-decisions.md`.
