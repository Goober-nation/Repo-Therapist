"""Closed registry for documentation-grading-v1 deduction facts."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Mapping

from .grading import COMPONENTS


@dataclass(frozen=True)
class Rule:
    checks: frozenset[str]
    required: frozenset[str]
    optional: frozenset[str]
    evidence: frozenset[str]


def _rule(checks: str, required: str, optional: str = "",
          evidence: str = "file_content document_review file_exists file_missing") -> Rule:
    return Rule(frozenset(checks.split()), frozenset(required.split()),
                frozenset(optional.split()), frozenset(evidence.split()))


RULES = {
    "DOC_README_MISSING": _rule("readme.exists", "searched_paths", evidence="file_missing"),
    "DOC_README_EMPTY": _rule("readme.exists", "path empty_reason", evidence="file_exists file_content"),
    "DOC_LICENSE_MISSING": _rule("license.exists", "searched_paths", evidence="file_missing"),
    "DOC_LICENSE_EMPTY": _rule("license.exists", "path empty_reason", evidence="file_exists file_content"),
    "DOC_CONTRIBUTING_MISSING": _rule("contributing.exists", "searched_paths", evidence="file_missing"),
    "DOC_CONTRIBUTING_EMPTY": _rule("contributing.exists", "path empty_reason", evidence="file_exists file_content"),
    "DOC_CODEOWNERS_MISSING": _rule("codeowners.exists", "searched_paths", evidence="file_missing"),
    "DOC_CODEOWNERS_EMPTY": _rule("codeowners.exists", "path empty_reason", evidence="file_exists file_content"),
    "DOC_PURPOSE_MISSING": _rule("purpose.summary", "missing_aspects reviewed_paths", evidence="document_review file_missing"),
    "DOC_PURPOSE_UNCLEAR": _rule("purpose.summary", "unclear_aspects", evidence="file_content document_review"),
    "DOC_CAPABILITIES_MISSING": _rule("purpose.capabilities", "coverage_level documented_capability_count reviewed_paths", evidence="document_review file_missing"),
    "DOC_CAPABILITIES_INCOMPLETE": _rule("purpose.capabilities", "coverage_level documented_capability_count"),
    "DOC_LOCAL_RUN_MISSING": _rule("local_run.prerequisites local_run.dependencies local_run.configuration local_run.execution local_run.verification", "missing_checks reviewed_paths", evidence="document_review file_missing"),
    "DOC_LOCAL_RUN_PREREQUISITES_INCOMPLETE": _rule("local_run.prerequisites", "missing_aspects", "detected_requirements"),
    "DOC_LOCAL_RUN_DEPENDENCIES_INCOMPLETE": _rule("local_run.dependencies", "missing_aspects", "detected_package_managers"),
    "DOC_LOCAL_RUN_CONFIGURATION_INCOMPLETE": _rule("local_run.configuration", "missing_aspects", "detected_configuration"),
    "DOC_LOCAL_RUN_COMMANDS_INCOMPLETE": _rule("local_run.execution", "missing_aspects documented_command_count", "detected_entrypoints"),
    "DOC_LOCAL_RUN_VERIFICATION_INCOMPLETE": _rule("local_run.verification", "missing_aspects documented_verification_count"),
    "DOC_BUILD_INSTRUCTIONS_MISSING": _rule("build.commands", "detected_build_systems reviewed_paths"),
    "DOC_BUILD_INSTRUCTIONS_INCOMPLETE": _rule("build.commands", "missing_aspects documented_command_count detected_build_systems"),
    "DOC_BUILD_CONFIGURATION_INCOMPLETE": _rule("build.configuration", "missing_aspects", "detected_configuration"),
    "DOC_BUILD_OUTPUT_UNCLEAR": _rule("build.output", "missing_aspects", "detected_artifacts"),
    "DOC_TESTING_INSTRUCTIONS_MISSING": _rule("testing.commands", "detected_test_types detected_test_commands reviewed_paths"),
    "DOC_TESTING_COMMANDS_INCOMPLETE": _rule("testing.commands", "missing_aspects documented_command_count detected_test_types"),
    "DOC_TESTING_ENVIRONMENT_INCOMPLETE": _rule("testing.environment", "missing_aspects", "detected_requirements"),
    "DOC_TESTING_SCOPE_UNCLEAR": _rule("testing.scope", "missing_aspects detected_test_types"),
    "DOC_USAGE_EXAMPLE_MISSING": _rule("usage.primary_example", "detected_interfaces reviewed_paths"),
    "DOC_USAGE_EXAMPLE_INCOMPLETE": _rule("usage.primary_example", "missing_aspects example_type", evidence="file_content"),
    "DOC_USAGE_INPUTS_UNCLEAR": _rule("usage.inputs", "missing_aspects", "detected_required_input_count"),
    "DOC_USAGE_RESULT_UNCLEAR": _rule("usage.expected_result", "missing_aspects", "detected_result_kind"),
    "DOC_STRUCTURE_OVERVIEW_MISSING": _rule("structure.overview", "detected_components reviewed_paths"),
    "DOC_STRUCTURE_OVERVIEW_INCOMPLETE": _rule("structure.overview", "missing_aspects documented_component_count detected_component_count"),
    "DOC_NAVIGATION_INCOMPLETE": _rule("structure.navigation", "missing_aspects", "unlinked_documents"),
    "DOC_REFERENCE_NOT_FOUND": _rule("consistency.references", "reference_kind source_path referenced_target", evidence="file_content file_missing"),
    "DOC_LINK_BROKEN": _rule("consistency.references", "link_kind source_path target", "anchor"),
    "DOC_COMMAND_INCONSISTENT": _rule("consistency.commands", "documented_command mismatch_kind"),
    "DOC_SCRIPT_NOT_FOUND": _rule("consistency.commands", "documented_command missing_script manifest_path"),
    "DOC_CONFIGURATION_INCONSISTENT": _rule("consistency.commands", "configuration_kind documented_identifier detected_identifiers"),
    "DOC_COMPONENT_DESCRIPTION_INCONSISTENT": _rule("consistency.description", "claim_kind documented_identifier mismatch_kind"),
}

_COUNT_FIELDS = {"documented_capability_count", "documented_command_count",
                 "documented_verification_count", "documented_component_count",
                 "detected_component_count", "detected_required_input_count"}
_LIST_FIELDS = {"searched_paths", "reviewed_paths", "missing_aspects", "unclear_aspects",
                "missing_checks", "detected_requirements", "detected_package_managers",
                "detected_configuration", "detected_entrypoints", "detected_build_systems",
                "detected_artifacts", "detected_test_types", "detected_test_commands",
                "detected_interfaces", "detected_components", "unlinked_documents",
                "detected_identifiers"}
_TRUSTED_FIELDS = {field for field in _LIST_FIELDS | _COUNT_FIELDS | {"detected_result_kind"}
                   if field.startswith("detected_")}
_STRING_FIELDS = {"path", "example_type", "reference_kind", "source_path", "referenced_target",
                  "link_kind", "target", "anchor", "documented_command", "mismatch_kind",
                  "missing_script", "manifest_path", "configuration_kind",
                  "documented_identifier", "claim_kind", "detected_result_kind"}
_TEST_TYPES = {"unit", "integration", "end_to_end", "contract", "smoke", "performance", "other"}
_INTERFACES = {"http_api", "library", "cli", "user_interface", "infrastructure",
               "template", "documentation", "other"}
_ENUMS = {
    "example_type": _INTERFACES,
    "reference_kind": {"file", "directory", "script", "configuration", "other"},
    "link_kind": {"file", "directory", "anchor"},
    "mismatch_kind": {"missing", "wrong_target", "wrong_parameter", "wrong_value", "other"},
    "configuration_kind": {"environment_variable", "config_file", "flag", "other"},
    "claim_kind": {"component", "capability", "technology"},
    "detected_result_kind": {"text", "json", "file", "http_response", "user_interface", "other"},
}
_REQUIRED_EVIDENCE = {
    "DOC_PURPOSE_UNCLEAR": {"file_content"},
    "DOC_CAPABILITIES_INCOMPLETE": {"file_content"},
    "DOC_REFERENCE_NOT_FOUND": {"file_content", "file_missing"},
    "DOC_LINK_BROKEN": {"file_content"},
    "DOC_COMMAND_INCONSISTENT": {"file_content"},
    "DOC_SCRIPT_NOT_FOUND": {"file_content"},
    "DOC_CONFIGURATION_INCONSISTENT": {"file_content"},
    "DOC_COMPONENT_DESCRIPTION_INCONSISTENT": {"file_content"},
}


def validate_deduction(check_id: str, deduction: dict, reviewed_paths: set[str],
                       all_paths: set[str], trusted_facts: Mapping[str, object] | None = None) -> None:
    """Reject unregistered codes, extra fields, invented detector facts and prose."""
    code = deduction["code"]
    rule = RULES.get(code)
    if rule is None or check_id not in rule.checks:
        raise ValueError(f"deduction code not registered for {check_id}: {code}")
    if set(deduction) != {"deduction_id", "code", "raw_points_lost", "facts", "evidence"}:
        raise ValueError("deduction contains unknown fields")
    facts = deduction["facts"]
    if not rule.required <= facts.keys() or not facts.keys() <= rule.required | rule.optional:
        raise ValueError(f"facts do not match schema for {code}")
    trusted_facts = trusted_facts or {}
    for name, value in facts.items():
        if name in _TRUSTED_FIELDS and (name not in trusted_facts or trusted_facts[name] != value):
            raise ValueError(f"unverified detector fact: {name}")
        if name in _COUNT_FIELDS:
            if type(value) is not int or value < 0:
                raise ValueError(f"invalid nonnegative count: {name}")
        elif name in _LIST_FIELDS:
            if not isinstance(value, list) or (not value and name != "reviewed_paths"):
                raise ValueError(f"invalid list fact: {name}")
            if name in _TRUSTED_FIELDS and name not in {"detected_test_types", "detected_identifiers"}:
                if any(not isinstance(item, dict) or set(item) != {"kind", "identifier", "path"}
                       or any(not isinstance(part, str) or not part or len(part) > 256
                              for part in item.values()) or item["path"] not in all_paths
                       for item in value):
                    raise ValueError(f"invalid detector entries: {name}")
            elif (any(not isinstance(item, str) or not item or len(item) > 256 for item in value)
                  or len(set(value)) != len(value)):
                raise ValueError(f"invalid list fact: {name}")
            if name in {"missing_aspects", "unclear_aspects"} and not set(value) <= COMPONENTS[check_id].keys():
                raise ValueError(f"unknown component in {name}")
            if name == "missing_checks" and not set(value) <= {
                "prerequisites", "dependencies", "configuration", "execution", "verification"
            }:
                raise ValueError("unknown local-run check")
            if name == "reviewed_paths" and not set(value) <= reviewed_paths:
                raise ValueError("reviewed path was not in complete model context")
            if name in {"searched_paths", "unlinked_documents"} and any(
                path.startswith("/") or ".." in path.split("/") for path in value
            ):
                raise ValueError("invalid path fact")
            if name == "detected_test_types" and not set(value) <= _TEST_TYPES:
                raise ValueError("unknown test type")
            if name == "detected_interfaces" and not {item["kind"] for item in value} <= _INTERFACES:
                raise ValueError("unknown interface kind")
        elif name in _STRING_FIELDS:
            if not isinstance(value, str) or not value or len(value) > 512:
                raise ValueError(f"invalid string fact: {name}")
            if name in _ENUMS and value not in _ENUMS[name]:
                raise ValueError(f"unknown enum value: {name}")
        elif name == "coverage_level":
            allowed = {"NONE"} if code == "DOC_CAPABILITIES_MISSING" else {"MINIMAL", "PARTIAL", "MOST"}
            if value not in allowed:
                raise ValueError("invalid coverage level")
        elif name == "empty_reason":
            if value not in {"EMPTY", "WHITESPACE_ONLY", "COMMENTS_ONLY", "TITLE_ONLY"}:
                raise ValueError("invalid empty reason")
        else:
            raise ValueError(f"unhandled fact schema field: {name}")
    if code == "DOC_CAPABILITIES_MISSING" and facts["documented_capability_count"] != 0:
        raise ValueError("missing capabilities must have zero documented count")
    for name in ("path", "source_path", "manifest_path"):
        if name in facts and facts[name] not in all_paths:
            raise ValueError(f"fact path does not exist: {name}")
    if "reviewed_paths" in facts:
        reviewed_evidence = {path for evidence in deduction["evidence"]
                             if evidence.get("type") == "document_review"
                             for path in evidence.get("paths", [])}
        if not set(facts["reviewed_paths"]) <= reviewed_evidence:
            raise ValueError("reviewed_paths are not supported by document_review evidence")
        if facts["reviewed_paths"] and not reviewed_evidence:
            raise ValueError("document_review evidence required for existing documentation")
        if not facts["reviewed_paths"]:
            if reviewed_paths or not any(item["type"] == "file_missing" and
                                         item["path"] == "README.md"
                                         for item in deduction["evidence"]):
                raise ValueError("missing-document evidence requires no selected docs and missing README")
    if not {item["type"] for item in deduction["evidence"]} <= rule.evidence:
        raise ValueError(f"evidence type not allowed for {code}")
    if not _REQUIRED_EVIDENCE.get(code, set()) <= {item["type"] for item in deduction["evidence"]}:
        raise ValueError(f"required evidence type missing for {code}")
    if rule.evidence == {"document_review"} and not any(
        item["type"] == "document_review" for item in deduction["evidence"]
    ):
        raise ValueError("document_review evidence required")
