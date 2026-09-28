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
display `name`; otherwise the Russian label for a known category ID (`activity`, `issues`,
`documentation`, `code_health`, `security`, `ci_cd`, see `src/metrics.ts`) or the key itself is
displayed. An object's `metrics` array (`{id, no_data, raw_value}` from grade-service) is shown
as the facts behind the score; shares (0–1) are shown in percent. Other fields are ignored.

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

Categories without data are listed under «Нет данных» with a reason: in a public analysis
Security and CI/CD are always listed as owner-only; otherwise the reason is that the source
returned nothing. `grade.missing_data` from the backend is authoritative for that list.

The public ranking has no category scores (Gateway returns only `public_score`). It is paged
on the server (`GET /ranking`, 100 per page): sorting and the language filter are server-side,
while text search and the minimum-score filter narrow the loaded page only and say so. A missing
overall score does not match a positive minimum-score filter.

## Verification

From `frontend`, run `corepack pnpm run check` (type check, tests, build). The tests cover
mapping, full-page rendering, Markdown and PDF export, category additions/removals, malformed
values, metric facts, missing data, pending/completed transitions and polling.
