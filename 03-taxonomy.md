# Taxonomy

The closed vocabulary every tagging output must be validated against.

## Categories
```
top, bottom, outerwear, footwear, headwear, accessory
```

### Subcategories
```
top: t-shirt, polo, shirt, sweater, hoodie, sweatshirt, tank-top
bottom: jeans, chinos, dress-pants, shorts, sweatpants
outerwear: jacket, coat, blazer, vest
footwear: sneakers, boots, dress-shoes, sandals, loafers
headwear: cap, beanie, hat
accessory: belt, scarf, tie, bag, watch, sunglasses, gloves
```

Every garment's `subcategory` must be one of the values listed under its
`category`. This list is scoped to a normal men's wardrobe as of first
cataloging — trim unused values or add missing ones once real items are
run through the pipeline. If the catalog later extends to a women's
wardrobe, extend this list (e.g. add a `dress` category, add subcategories
like `skirt` under `bottom`) rather than restructuring it.

## Color palette
```
black, white, gray, navy, blue, red, green, olive, brown, tan, beige,
burgundy, pink, purple, yellow, orange
```

## Pattern
```
solid, striped, plaid, print
```
Covers garments where color alone doesn't describe the item — e.g. a
navy/white striped shirt is `pattern: striped`, `dominant_color: navy`,
`secondary_colors: [white]`, rather than trying to encode the stripe
structure into color fields alone.

## Warmth tiers
```
light, medium, heavy
```

## Formality
```
casual, smart-casual, formal
```

## Change process
This file is the contract for `05-vlm-tagging-spec.md`'s output schema. If
you change a value here, the tagging spec's enum must be updated in the
same ticket — don't let them drift.
