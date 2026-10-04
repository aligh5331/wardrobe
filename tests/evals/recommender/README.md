# Recommender eval forms

Each `<YYYY-MM-DD-HHMM>-<label>.md` file here is one run of the recommender
eval (ING-059, `docs/adr/0002-recommender-eval-is-human-rated.md`). The eval
runs the real recommender against your configured LLM on a fixed synthetic
"trap wardrobe", in 4 weather scenarios, 3 times each. Forms hold only
synthetic items and the model name, so they are safe to commit.

## 1. Run the eval

From the repo root, with `LLM_URL` set in `.env` (or in the environment):

```
EVAL_LABEL=baseline go test -tags=integration -timeout 30m ./tests/ -run 'TestRecommendEval$' -v
```

- `EVAL_LABEL` names the form (lowercased; anything outside `a-z 0-9 -`
  becomes `-`). Without it, the short commit hash is used.
- It makes 12 LLM calls, or up to 24 with retries. `-timeout 30m` covers the
  120 s LLM timeout. Without `LLM_URL` the test skips.
- The test fails only when a run returns no valid outfits. The form is
  written either way.
- The log ends with the auto-flag table, which is also at the bottom of the
  form.

## 2. Fill in the form

For each outfit, replace the `_`:

- `- Score (1-5): 4`, where 5 means you'd wear it as is.
- `- Would wear (y/n): y`
- `- Note:` is optional free text: what's off, or what's good.

At the end of each scenario, rate `- Variety (1-5): _` for how different the
3 outfits feel from each other.

You rate the first valid run of each scenario. The auto-flag table counts
rule breaks across all 3 runs. Auto-flags are hints, not verdicts: your score
is what counts.

## 3. Compare forms

```
go test -tags=integration ./tests/ -run 'TestRecommendEvalSummary$' -v
```

This makes no LLM call. It prints one row per form that has at least one
rating, oldest first: label, prompt fingerprint, model, rated outfits,
average score, would-wear %, average variety, valid runs and total
auto-flags. A rating outside the allowed values fails it and names the file
and line.

The fingerprint changes whenever the prompt text, the trap wardrobe or the
scenarios change. Only compare forms whose differences you know.
