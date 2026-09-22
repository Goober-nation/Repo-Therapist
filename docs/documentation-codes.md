# Documentation deduction registry (`documentation-grading-v1`)

This registry defines the machine-readable deductions emitted by LLM service. A grading response
contains no user-facing titles, explanations, or recommendations. Unknown codes, fact fields, enum
values, or evidence variants are invalid.

## Common deduction shape

```json
{
  "deduction_id": "local_run.configuration:required-parameters",
  "code": "DOC_LOCAL_RUN_CONFIGURATION_INCOMPLETE",
  "raw_points_lost": 2,
  "facts": {
    "missing_aspects": ["required_parameters"]
  },
  "evidence": []
}
```

- `deduction_id` is stable and unique within the result;
- `raw_points_lost` is a positive integer;
- `facts` follows the registered schema and rejects additional properties;
- `missing_aspects` and `unclear_aspects` contain only component IDs registered for the source
  check in `docs/documentation-grading.md`;
- detector facts (`detected_*`) come from ordinary code, not free-form model output. The model
  response omits them; the service inserts verified values after parsing the response;
- at least one validated evidence item is required.

## Evidence variants

| Type | Required fields | Use |
|---|---|---|
| `file_content` | `path`, `start_line`, `end_line`, `quote` | Exact positive or contradictory text |
| `file_exists` | `path` | Presence of a repository artifact |
| `file_missing` | `path` | Absence proven against the complete manifest |
| `document_review` | `paths`, `context_sha256` | Absence of semantic content in fully reviewed documents |

`document_review.paths` must be non-empty. Every path must exist in the manifest and must have been
included completely in the context identified by `context_sha256`. It is the required evidence for
claims such as “the purpose is not documented”; `file_missing` cannot prove absence of a section in
an existing document.

If the complete repository listing contains no selected user documentation, a code whose facts
require `reviewed_paths` may instead set `reviewed_paths: []` and use `file_missing` for `README.md`.
This exception does not apply when any user documentation was included completely in the model
context; then `document_review` is required. A nonexistent file is never marked as reviewed.

## File checks

| Code | Allowed check | Required facts | Evidence | Minimum severity |
|---|---|---|---|---|
| `DOC_README_MISSING` | `readme.exists` | `searched_paths` | `file_missing` | `HIGH` |
| `DOC_README_EMPTY` | `readme.exists` | `path`, `empty_reason` | `file_content` or `file_exists` | `HIGH` |
| `DOC_LICENSE_MISSING` | `license.exists` | `searched_paths` | `file_missing` | `HIGH` |
| `DOC_LICENSE_EMPTY` | `license.exists` | `path`, `empty_reason` | `file_content` or `file_exists` | `HIGH` |
| `DOC_CONTRIBUTING_MISSING` | `contributing.exists` | `searched_paths` | `file_missing` | `LOW` |
| `DOC_CONTRIBUTING_EMPTY` | `contributing.exists` | `path`, `empty_reason` | `file_content` or `file_exists` | `LOW` |
| `DOC_CODEOWNERS_MISSING` | `codeowners.exists` | `searched_paths` | `file_missing` | `LOW` |
| `DOC_CODEOWNERS_EMPTY` | `codeowners.exists` | `path`, `empty_reason` | `file_content` or `file_exists` | `LOW` |

`empty_reason` is one of `EMPTY`, `WHITESPACE_ONLY`, `COMMENTS_ONLY`, or `TITLE_ONLY`.

## Purpose and capabilities

| Code | Allowed check | Required facts | Evidence | Minimum severity |
|---|---|---|---|---|
| `DOC_PURPOSE_MISSING` | `purpose.summary` | `missing_aspects`, `reviewed_paths` | `document_review` | `LOW` |
| `DOC_PURPOSE_UNCLEAR` | `purpose.summary` | `unclear_aspects` | `file_content`; optional `document_review` | `LOW` |
| `DOC_CAPABILITIES_MISSING` | `purpose.capabilities` | `coverage_level=NONE`, `documented_capability_count=0`, `reviewed_paths` | `document_review` | `LOW` |
| `DOC_CAPABILITIES_INCOMPLETE` | `purpose.capabilities` | `coverage_level`, `documented_capability_count` | `file_content`, `document_review` | `LOW` |

Incomplete `coverage_level` is one of `MINIMAL`, `PARTIAL`, or `MOST`.

## Local setup and run

| Code | Allowed checks | Required facts | Evidence | Minimum severity |
|---|---|---|---|---|
| `DOC_LOCAL_RUN_MISSING` | any `local_run.*` | `missing_checks`, `reviewed_paths` | `document_review` | `LOW` |
| `DOC_LOCAL_RUN_PREREQUISITES_INCOMPLETE` | `local_run.prerequisites` | `missing_aspects`, optional `detected_requirements` | documentation plus detector evidence | `LOW` |
| `DOC_LOCAL_RUN_DEPENDENCIES_INCOMPLETE` | `local_run.dependencies` | `missing_aspects`, optional `detected_package_managers` | documentation plus manifest evidence | `LOW` |
| `DOC_LOCAL_RUN_CONFIGURATION_INCOMPLETE` | `local_run.configuration` | `missing_aspects`, optional `detected_configuration` | documentation plus configuration evidence | `LOW` |
| `DOC_LOCAL_RUN_COMMANDS_INCOMPLETE` | `local_run.execution` | `missing_aspects`, `documented_command_count`, optional `detected_entrypoints` | documentation plus entrypoint evidence | `LOW` |
| `DOC_LOCAL_RUN_VERIFICATION_INCOMPLETE` | `local_run.verification` | `missing_aspects`, `documented_verification_count` | `file_content` or `document_review` | `LOW` |

`missing_checks` uses `prerequisites`, `dependencies`, `configuration`, `execution`, and
`verification`. Detector entries use a controlled `kind`, stable identifier, and source path. Secret
values are never included; environment-variable names are allowed.

## Build

| Code | Allowed check | Required facts | Evidence | Minimum severity |
|---|---|---|---|---|
| `DOC_BUILD_INSTRUCTIONS_MISSING` | `build.commands` | `detected_build_systems`, `reviewed_paths` | build artifact plus `document_review` | `LOW` |
| `DOC_BUILD_INSTRUCTIONS_INCOMPLETE` | `build.commands` | `missing_aspects`, `documented_command_count`, `detected_build_systems` | documentation plus build evidence | `LOW` |
| `DOC_BUILD_CONFIGURATION_INCOMPLETE` | `build.configuration` | `missing_aspects`, optional `detected_configuration` | documentation plus build evidence | `LOW` |
| `DOC_BUILD_OUTPUT_UNCLEAR` | `build.output` | `missing_aspects`, optional `detected_artifacts` | documentation plus build evidence | `LOW` |

Artifact kinds are `binary`, `container_image`, `package`, `archive`, `frontend_bundle`,
`generated_documentation`, or `other`.

## Testing

| Code | Allowed check | Required facts | Evidence | Minimum severity |
|---|---|---|---|---|
| `DOC_TESTING_INSTRUCTIONS_MISSING` | `testing.commands` | `detected_test_types`, `detected_test_commands`, `reviewed_paths` | test artifact plus `document_review` | `LOW` |
| `DOC_TESTING_COMMANDS_INCOMPLETE` | `testing.commands` | `missing_aspects`, `documented_command_count`, `detected_test_types` | documentation plus test evidence | `LOW` |
| `DOC_TESTING_ENVIRONMENT_INCOMPLETE` | `testing.environment` | `missing_aspects`, optional `detected_requirements` | documentation plus test configuration | `LOW` |
| `DOC_TESTING_SCOPE_UNCLEAR` | `testing.scope` | `missing_aspects`, `detected_test_types` | documentation plus test evidence | `LOW` |

Test types are `unit`, `integration`, `end_to_end`, `contract`, `smoke`, `performance`, or `other`.
`detected_test_commands` may be an empty list when test files exist but no explicit command was
found in selected manifests or CI configuration. An empty list does not mean no command exists
anywhere in the repository.

## Usage

| Code | Allowed check | Required facts | Evidence | Minimum severity |
|---|---|---|---|---|
| `DOC_USAGE_EXAMPLE_MISSING` | `usage.primary_example` | `detected_interfaces`, `reviewed_paths` | interface evidence plus `document_review` | `LOW` |
| `DOC_USAGE_EXAMPLE_INCOMPLETE` | `usage.primary_example` | `missing_aspects`, `example_type` | `file_content` | `LOW` |
| `DOC_USAGE_INPUTS_UNCLEAR` | `usage.inputs` | `missing_aspects`, optional `detected_required_input_count` | documentation plus interface evidence | `LOW` |
| `DOC_USAGE_RESULT_UNCLEAR` | `usage.expected_result` | `missing_aspects`, optional `detected_result_kind` | documentation plus interface evidence | `LOW` |

Interface and example kinds are controlled enums covering HTTP APIs, libraries, CLIs, user
interfaces, infrastructure, templates, documentation, and `other`.
Their values are `http_api`, `library`, `cli`, `user_interface`, `infrastructure`, `template`,
`documentation`, and `other`.

## Structure and navigation

| Code | Allowed check | Required facts | Evidence | Minimum severity |
|---|---|---|---|---|
| `DOC_STRUCTURE_OVERVIEW_MISSING` | `structure.overview` | `detected_components`, `reviewed_paths` | component evidence plus `document_review` | `LOW` |
| `DOC_STRUCTURE_OVERVIEW_INCOMPLETE` | `structure.overview` | `missing_aspects`, `documented_component_count`, `detected_component_count` | documentation plus component evidence | `LOW` |
| `DOC_NAVIGATION_INCOMPLETE` | `structure.navigation` | `missing_aspects`, optional `unlinked_documents` | documentation and file evidence | `LOW` |

## Consistency

| Code | Allowed check | Required facts | Evidence | Minimum severity |
|---|---|---|---|---|
| `DOC_REFERENCE_NOT_FOUND` | `consistency.references` | `reference_kind`, `source_path`, `referenced_target` | `file_content`, `file_missing` | `LOW` |
| `DOC_LINK_BROKEN` | `consistency.references` | `link_kind`, `source_path`, `target`, optional `anchor` | source and target evidence | `LOW` |
| `DOC_COMMAND_INCONSISTENT` | `consistency.commands` | `documented_command`, `mismatch_kind` | documentation plus manifest evidence | `LOW` |
| `DOC_SCRIPT_NOT_FOUND` | `consistency.commands` | `documented_command`, `missing_script`, `manifest_path` | documentation and full scripts section | `LOW` |
| `DOC_CONFIGURATION_INCONSISTENT` | `consistency.commands` | `configuration_kind`, `documented_identifier`, `detected_identifiers` | documentation plus configuration evidence | `LOW` |
| `DOC_COMPONENT_DESCRIPTION_INCONSISTENT` | `consistency.description` | `claim_kind`, `documented_identifier`, `mismatch_kind` | claim plus repository evidence | `LOW` |

External HTTP links are not checked in v1; network availability must not affect a repository score.

Other v1 fact enums are closed: `reference_kind` is `file`, `directory`, `script`,
`configuration`, or `other`; `link_kind` is `file`, `directory`, or `anchor`;
`mismatch_kind` is `missing`, `wrong_target`, `wrong_parameter`, `wrong_value`, or `other`;
`configuration_kind` is `environment_variable`, `config_file`, `flag`, or `other`;
`claim_kind` is `component`, `capability`, or `technology`; `detected_result_kind` is
`text`, `json`, `file`, `http_response`, `user_interface`, or `other`.

## Group problem codes

Service code may replace several related deduction codes with one problem code while preserving all
source deduction IDs and their evidence:

- `DOC_DOCUMENTATION_MISSING`;
- `DOC_LOCAL_RUN_INCOMPLETE`;
- `DOC_BUILD_INCOMPLETE`;
- `DOC_TESTING_INCOMPLETE`;
- `DOC_USAGE_INCOMPLETE`;
- `DOC_STRUCTURE_INCOMPLETE`;
- `DOC_DOCUMENTATION_OUTDATED`.

Group codes never create additional point loss. Their `category_points_lost` is the normalized sum
of source deductions. `DOC_DOCUMENTATION_OUTDATED` uses `mismatch_count` and `affected_areas`;
other group codes use `affected_checks` and the union of registered `missing_aspects`.
