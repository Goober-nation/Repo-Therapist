# Documentation grading method (`documentation-grading-v1`)

> Current active output: `documentation.analysis.completed` v2 contains only `grade`,
> eight `subcategories`, and a flat `facts` list. Internal checks/components and
> numeric deduction impacts are not transmitted. The active model path checks format
> only; strict evidence/impact rules below describe the legacy validator and scoring
> guidance, not acceptance requirements for the current Yandex response. See
> [Kafka contract](kafka-contracts.md) and [active model output](llm-model-output.md).

This document defines the reproducible 0–100 method owned by LLM service for the
`documentation` category. Kafka transport is defined in `docs/kafka-contracts.md`.

The method combines deterministic repository checks with semantic checks. The model assigns only
integer points and machine-readable deductions. It does not write explanations or recommendations
during grading; those are generated later by the report workflow.

## Score composition

| Group | Checks | Maximum |
|---|---|---:|
| Required files | README, LICENSE, CONTRIBUTING, CODEOWNERS | 20 |
| Purpose and overview | Purpose, capabilities | 15 |
| Local setup and run | Prerequisites, dependencies, configuration, execution, verification | 20 |
| Build | Commands, configuration, output | 10 |
| Testing | Commands, environment, scope | 10 |
| Usage examples | Primary example, inputs, expected result | 10 |
| Structure and navigation | Repository overview, documentation navigation | 5 |
| Consistency | References, commands, description | 10 |
| **Total** |  | **100** |

## Check outcomes and normalization

Every check has one outcome:

- `EVALUATED`: the check was performed; `points_awarded` is an integer from zero through
  `max_points`;
- `NOT_APPLICABLE`: the check is not relevant to this repository;
- `NO_DATA`: the check is relevant, but the available snapshot or context cannot support a reliable
  decision.

Missing expected documentation is an evaluated zero, not `NO_DATA`. For example, a missing README
or absent local-run instruction loses points. A repository with no separate build process may mark
Build checks `NOT_APPLICABLE` without losing points.

The service calculates:

```text
applicable_max = sum(max_points where outcome != NOT_APPLICABLE)
evaluated_max  = sum(max_points where outcome == EVALUATED)
earned         = sum(points_awarded where outcome == EVALUATED)

coverage = evaluated_max / applicable_max * 100
score    = earned / evaluated_max * 100
```

The score is rounded to two decimal places. Missing data never counts as zero and never reduces the
score. A result is `CALCULATED` when all applicable points were evaluated and
`CALCULATED_PARTIAL` otherwise. Both statuses carry a numeric score. If no applicable check can be
evaluated, the workflow fails instead of fabricating a number.

## Deterministic checks (20 points)

| `check_id` | Maximum | Pass condition | Missing / empty codes |
|---|---:|---|---|
| `readme.exists` | 5 | A non-empty root README exists | `DOC_README_MISSING`, `DOC_README_EMPTY` |
| `license.exists` | 5 | A non-empty root license file exists | `DOC_LICENSE_MISSING`, `DOC_LICENSE_EMPTY` |
| `contributing.exists` | 5 | A non-empty contribution guide exists | `DOC_CONTRIBUTING_MISSING`, `DOC_CONTRIBUTING_EMPTY` |
| `codeowners.exists` | 5 | A non-empty CODEOWNERS file exists | `DOC_CODEOWNERS_MISSING`, `DOC_CODEOWNERS_EMPTY` |

Each check awards either zero or five points. Filename matching is case-insensitive. Accepted
locations and names are:

- README: repository root; `README`, `README.md`, `README.rst`, or `README.adoc`;
- license: repository root; `LICENSE`, `LICENSE.md`, `LICENSE.txt`, `LICENCE`, and matching
  extension variants;
- contribution guide: repository root, `.github/`, or `docs/`; filename `CONTRIBUTING` with an
  optional common documentation extension;
- CODEOWNERS: repository root, `.github/`, or `docs/`; filename `CODEOWNERS`.

A file containing only whitespace, comments, or a title is empty for this method. Missing or empty
README and LICENSE deductions have minimum severity `HIGH`; CONTRIBUTING and CODEOWNERS have
minimum severity `LOW`.

## Semantic and hybrid checks (80 points)

The model may award any integer from zero through the listed maximum. Prompts provide anchored
rubrics for every integer; application code validates the bounds and recomputes the final score.

The model scores smaller components rather than inventing a check total. Service code sums the
component points. Unless a component has a more specific rule below, these anchors apply:

| Component maximum | Anchors |
|---:|---|
| 1 | `0` absent or unusable; `1` present and sufficient, or demonstrably not required |
| 2 | `0` absent; `1` partial or ambiguous; `2` clear and sufficient |
| 3 | `0` absent; `1` fragmentary; `2` usable with material gaps; `3` complete |
| 4 | `0` absent; `1` minimal; `2` partial; `3` covers most primary cases; `4` complete |

The allowed component IDs below are part of `documentation-grading-v1`.

### Purpose and overview (15 points)

| `check_id` | Maximum | Requirement |
|---|---:|---|
| `purpose.summary` | 8 | The documentation makes clear what the project is, which problem it solves, and what result it provides. |
| `purpose.capabilities` | 7 | The main functions or capabilities are described. |

| Check | Scoring components |
|---|---|
| `purpose.summary` | `project_identity` 0–1; `problem_statement` 0–2; `solution_role` 0–2; `expected_outcome` 0–2; `summary_clarity` 0–1 |
| `purpose.capabilities` | `capability_coverage` 0–4; `capability_clarity` 0–3 |

Codes: `DOC_PURPOSE_MISSING`, `DOC_PURPOSE_UNCLEAR`, `DOC_CAPABILITIES_MISSING`, and
`DOC_CAPABILITIES_INCOMPLETE`. Describing a target audience is useful context but is not scored.
Accuracy against the repository is scored only under Consistency.

### Local setup and run (20 points)

| `check_id` | Maximum | Requirement |
|---|---:|---|
| `local_run.prerequisites` | 3 | Required tools, platforms, and relevant versions are stated. |
| `local_run.dependencies` | 4 | Dependency installation is described. |
| `local_run.configuration` | 4 | Required configuration, environment variables, and secret placeholders are described. |
| `local_run.execution` | 6 | A complete sequence of launch or first-use commands is provided. |
| `local_run.verification` | 3 | The reader can recognize a successful launch or setup. |

| Check | Scoring components |
|---|---|
| `local_run.prerequisites` | `required_tools` 0–1; `required_versions` 0–1; `external_requirements` 0–1 |
| `local_run.dependencies` | `installation_command` 0–2; `installation_context` 0–1; `dependency_variants` 0–1 |
| `local_run.configuration` | `required_parameters` 0–2; `configuration_steps` 0–1; `secret_handling` 0–1 |
| `local_run.execution` | `execution_commands` 0–3; `execution_sequence` 0–2; `runtime_entrypoint` 0–1 |
| `local_run.verification` | `success_indicator` 0–2; `smoke_check` 0–1 |

Codes: `DOC_LOCAL_RUN_MISSING`, `DOC_LOCAL_RUN_PREREQUISITES_INCOMPLETE`,
`DOC_LOCAL_RUN_DEPENDENCIES_INCOMPLETE`, `DOC_LOCAL_RUN_CONFIGURATION_INCOMPLETE`,
`DOC_LOCAL_RUN_COMMANDS_INCOMPLETE`, and `DOC_LOCAL_RUN_VERIFICATION_INCOMPLETE`.

The runnable path depends on repository type: service launch, library installation and first use,
CLI invocation, infrastructure validation/application, or local documentation rendering. A
requirement that the repository demonstrably does not need receives full points rather than a
fabricated deduction.

### Build (10 points)

| `check_id` | Maximum | Requirement |
|---|---:|---|
| `build.commands` | 6 | Exact build commands and their order are documented. |
| `build.configuration` | 2 | Required targets, profiles, flags, or build variables are documented. |
| `build.output` | 2 | The resulting artifact and its location are clear. |

| Check | Scoring components |
|---|---|
| `build.commands` | `primary_command` 0–3; `build_sequence` 0–2; `required_targets` 0–1 |
| `build.configuration` | `build_parameters` 0–1; `build_profiles` 0–1 |
| `build.output` | `artifact_identity` 0–1; `artifact_location` 0–1 |

Codes: `DOC_BUILD_INSTRUCTIONS_MISSING`, `DOC_BUILD_INSTRUCTIONS_INCOMPLETE`,
`DOC_BUILD_CONFIGURATION_INCOMPLETE`, and `DOC_BUILD_OUTPUT_UNCLEAR`.

Ordinary code first detects whether the repository has a distinct build process. If none exists,
all Build checks are `NOT_APPLICABLE`. A detected but undocumented build process is evaluated at
zero where appropriate.

### Testing (10 points)

| `check_id` | Maximum | Requirement |
|---|---:|---|
| `testing.commands` | 5 | Exact test commands are documented. |
| `testing.environment` | 3 | Required services, variables, fixtures, and setup are documented. |
| `testing.scope` | 2 | The available test groups and the scope of commands are clear. |

| Check | Scoring components |
|---|---|
| `testing.commands` | `primary_test_command` 0–3; `command_context` 0–1; `test_variants` 0–1 |
| `testing.environment` | `test_services` 0–1; `test_configuration` 0–1; `test_fixtures` 0–1 |
| `testing.scope` | `test_types` 0–1; `command_scope` 0–1 |

Codes: `DOC_TESTING_INSTRUCTIONS_MISSING`, `DOC_TESTING_COMMANDS_INCOMPLETE`,
`DOC_TESTING_ENVIRONMENT_INCOMPLETE`, and `DOC_TESTING_SCOPE_UNCLEAR`.

Ordinary code detects test files, framework configuration, scripts, and CI test stages. If no test
suite exists, these checks are `NOT_APPLICABLE`; the absence of tests belongs to Code Health or
CI/CD, not Documentation.

### Usage examples (10 points)

| `check_id` | Maximum | Requirement |
|---|---:|---|
| `usage.primary_example` | 5 | A practical example covers the primary usage scenario. |
| `usage.inputs` | 3 | Required inputs, parameters, or user actions are described. |
| `usage.expected_result` | 2 | The expected output or observable result is shown or explained. |

| Check | Scoring components |
|---|---|
| `usage.primary_example` | `executable_example` 0–3; `primary_scenario` 0–1; `usage_context` 0–1 |
| `usage.inputs` | `required_inputs` 0–2; `input_format` 0–1 |
| `usage.expected_result` | `result_form` 0–1; `result_example` 0–1 |

Codes: `DOC_USAGE_EXAMPLE_MISSING`, `DOC_USAGE_EXAMPLE_INCOMPLETE`,
`DOC_USAGE_INPUTS_UNCLEAR`, and `DOC_USAGE_RESULT_UNCLEAR`.

Local setup answers how to start or attach the project; Usage answers what to do with it afterwards.
The existence and completeness of an example are scored here. Whether its claims match the
repository is scored only under Consistency.

### Structure and navigation (5 points)

| `check_id` | Maximum | Requirement |
|---|---:|---|
| `structure.overview` | 3 | The roles of meaningful directories, modules, packages, or services are explained. |
| `structure.navigation` | 2 | Readers can reach important sections and supporting documents from the primary documentation. |

| Check | Scoring components |
|---|---|
| `structure.overview` | `component_map` 0–2; `component_roles` 0–1 |
| `structure.navigation` | `important_documents` 0–1; `section_navigation` 0–1 |

Codes: `DOC_STRUCTURE_OVERVIEW_MISSING`, `DOC_STRUCTURE_OVERVIEW_INCOMPLETE`, and
`DOC_NAVIGATION_INCOMPLETE`.

An overview is `NOT_APPLICABLE` for a small, self-evident repository. Navigation is
`NOT_APPLICABLE` for short, self-contained documentation. Broken targets are scored under
Consistency instead of being deducted twice.

### Consistency (10 points)

| `check_id` | Type | Maximum | Requirement |
|---|---|---:|---|
| `consistency.references` | `HYBRID` | 3 | Referenced files, directories, and internal links exist. |
| `consistency.commands` | `HYBRID` | 4 | Documented commands, scripts, parameters, and configuration match repository manifests. |
| `consistency.description` | `HYBRID` | 3 | Described components and capabilities match repository content. |

| Check | Scoring components |
|---|---|
| `consistency.references` | `file_targets` 0–2; `link_targets` 0–1 |
| `consistency.commands` | `command_targets` 0–2; `command_parameters` 0–1; `configuration_references` 0–1 |
| `consistency.description` | `described_components` 0–1; `described_capabilities` 0–1; `described_technologies` 0–1 |

Codes: `DOC_REFERENCE_NOT_FOUND`, `DOC_LINK_BROKEN`, `DOC_COMMAND_INCONSISTENT`,
`DOC_SCRIPT_NOT_FOUND`, `DOC_CONFIGURATION_INCONSISTENT`,
and `DOC_COMPONENT_DESCRIPTION_INCONSISTENT`.

The model extracts claims and ordinary code verifies them against the immutable snapshot. The
service never executes untrusted repository commands. If there is no substantive documentation to
cross-check, these checks are `NOT_APPLICABLE`; absence was already scored by the relevant checks.

## Deductions, evidence, and problems

Every evaluated check satisfies:

```text
sum(deduction.raw_points_lost) = max_points - points_awarded
```

Each deduction contains a stable `deduction_id`, a registered `code`, integer `raw_points_lost`,
code-specific typed `facts`, and validated evidence. It contains no user-facing title, reason, or
recommendation. The code registry and exact facts/evidence schemas are defined in
[`documentation-codes.md`](documentation-codes.md).

Normal code groups deductions by a shared root cause. Every deduction maps to exactly one problem
and can never be omitted or counted twice. Grouping never crosses unrelated semantic sections.
Repeated evidence is deduplicated. A grouped problem may use a registered group code, such as
`DOC_LOCAL_RUN_INCOMPLETE`, while preserving all original deduction IDs. Other registered group
codes are `DOC_BUILD_INCOMPLETE`, `DOC_TESTING_INCOMPLETE`, `DOC_USAGE_INCOMPLETE`,
`DOC_STRUCTURE_INCOMPLETE`, and `DOC_DOCUMENTATION_OUTDATED`. The normalized impact is:

```text
category_points_lost = sum(raw_points_lost) / evaluated_max * 100
```

Impacts are rounded to two decimals; the final problem receives any rounding remainder so that all
problem impacts sum exactly to `100 - score`.

Problem severity is computed after grouping from the problem's normalized impact:

| `category_points_lost` | Impact severity |
|---:|---|
| up to and including 10 | `LOW` |
| greater than 10 and up to 25 | `MEDIUM` |
| greater than 25 and up to 50 | `HIGH` |
| greater than 50 | `CRITICAL` |

The final severity is the higher of impact severity and the code's configured minimum severity.
Related missing purpose, local-run, and usage material may be represented by
`DOC_DOCUMENTATION_MISSING` when the repository has practically no user documentation; underlying
checks and deductions remain available for a complete audit trail.

## Missing data and control cases

Missing-data checks do not create scoring problems. They are reported through coverage, warnings,
and the final report's missing-data section.

Expected control cases:

| Scenario | Expected behavior |
|---|---|
| All applicable checks pass | `CALCULATED`, score 100, no problems |
| No distinct build or tests | Those checks are `NOT_APPLICABLE`; remaining points are renormalized |
| Applicable checks total 80, earned points total 72 | Score 90 |
| Applicable points total 100, evaluated points total 65, earned points total 52 | `CALCULATED_PARTIAL`, coverage 65%, score 80 |
| Expected README or local-run instructions are absent | Evaluated zero and a scoring deduction, never `NO_DATA` |
| Snapshot identity or integrity cannot be verified | Failed workflow; no fabricated completed result |
