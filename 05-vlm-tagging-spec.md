# VLM tagging spec

## Input
One photo of one garment, flat lay or on a hanger.

## Model
Qwen3-VL-8B, quantized (Q4), served locally via llama.cpp (`llama-server`
with `--mmproj`), run on-demand during cataloging sessions — not an
always-on service.

## Prompt (draft — refine once you see real outputs)
```
You are tagging a single clothing item photo for a personal wardrobe
catalog. Respond with ONLY a JSON object matching this schema, no other
text:

{
  "category": one of [top, bottom, outerwear, footwear, headwear, accessory],
  "subcategory": one of the subcategories valid for the chosen category
    (see list below — required, must not be null),
  "dominant_color": one of [black, white, gray, navy, blue, red, green,
    olive, brown, tan, beige, burgundy, pink, purple, yellow, orange],
  "secondary_colors": array of the same color enum, [] if none,
  "pattern": one of [solid, striped, plaid, print] — required, must not
    be null,
  "warmth_tier": one of [light, medium, heavy],
  "formality": one of [casual, smart-casual, formal]
}

Valid subcategories per category:
top: t-shirt, polo, shirt, sweater, hoodie, sweatshirt, tank-top
bottom: jeans, chinos, dress-pants, shorts, sweatpants
outerwear: jacket, coat, blazer, vest
footwear: sneakers, boots, dress-shoes, sandals, loafers
headwear: cap, beanie, hat
accessory: belt, scarf, tie, bag, watch, sunglasses, gloves

If uncertain about a field, make your best guess rather than omitting it.
subcategory is required — always choose the closest match from the list
above for the chosen category. pattern is also required — if the item is
a single solid color with no visible stripe/plaid/print structure, use
"solid" rather than omitting the field.
```

## Output validation
Every field must validate against `03-taxonomy.md`'s enums before being
written to the DB. Reject and retry (or flag for manual review) on:
- invalid enum value (including a `subcategory` that doesn't belong to
  the record's `category`)
- malformed JSON
- missing required field (`subcategory` and `pattern` included — neither
  may be null or omitted)

## Acceptance criteria (for the first ingestion ticket)
```
Given a garment photo at a given path
When it is passed through the tagging pipeline
Then a JSON record is produced that validates against 04-data-schema.md
And the record is written to the catalog store
And the original photo path is preserved on the record
```

## Known limitations to expect (not bugs)
- Subcategory accuracy will be weaker than category accuracy
- Fabric/texture is not asked for — genuinely unreliable from a single photo
- Color naming may need a few iterations of prompt tuning to converge with
  your actual taste in color names
- Pattern detection on busy or unusual prints may default to `print` as a
  catch-all — expect some manual correction early on
