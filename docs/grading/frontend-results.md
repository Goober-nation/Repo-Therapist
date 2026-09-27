# Dynamic category results in the frontend

The results component accepts any category names and any number of categories. The backend
still computes the grades using the existing category formulas and weights. This change only
affects how received grades are validated and displayed; the frontend does not recompute the
overall health score.

## Supported input

`category_scores` accepts a dictionary or the documented array of dictionaries. Values can
be numbers, `null`, or objects with `score` and optional `name`, `description`, and `trend`.
`criteria` is accepted as an alternative container; if both are supplied, `category_scores`
takes precedence, including when it is empty or `null`.

```json
{
  "status": "FINISHED",
  "health_score": 82,
  "category_scores": {
    "Мяу": { "score": 72, "description": "Explanation from the backend" },
    "Architecture": 90,
    "Tests": null
  }
}
```

The keys are preserved exactly. No category allowlist is applied. Objects can supply a
display `name`; otherwise the key is displayed. Localized names and descriptions for demo
data live in the demo fixtures. Extra fields are ignored; arbitrary nesting is not inferred.

## How a category reaches the screen

1. The backend calculates its score using that category's formula.
2. The frontend reads each category entry from the supplied container.
3. A finite number from 0 through 100 is rounded for display, as before. Zero is valid.
4. A missing, null, nonnumeric or out-of-range score displays `Нет данных`, without a score bar.
5. The component creates one card per category, using its name and optional description.
   The Markdown export uses the same names and scores.

There are no fabricated zero scores for absent categories. A completed response replaces the
previous set of categories, including clearing it when no categories are supplied. A pending
response without a category container retains the previous results; an explicitly empty
container clears them. In the array form, the last entry wins for a repeated key.

The security screen and existing category filters still have specific meanings. They use
optional lookups for `security`/`appsec`, `activity`, and `issues`/`community` without renaming
the data. Missing scores display as unavailable, sort below valid zeroes, and do not match
a positive minimum-score filter. An inactive filter does not exclude missing scores.

## Verification

From `frontend`, run `corepack pnpm test`, `corepack pnpm exec tsc --noEmit`, and
`corepack pnpm run build`. The tests cover mapping, full-page rendering, Markdown export,
category additions/removals, malformed values, and pending/completed transitions.
