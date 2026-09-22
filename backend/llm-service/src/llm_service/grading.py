"""Versioned, provider-independent Documentation scoring core.

This module does not call a model or publish Kafka events. It validates model check
results before computing scores, so a provider can never set the final grade directly.
"""

from __future__ import annotations

from dataclasses import dataclass
from decimal import Decimal, ROUND_HALF_UP
from hashlib import sha256
from pathlib import PurePosixPath
from typing import Mapping

METHOD_VERSION = "documentation-grading-v1"


@dataclass(frozen=True)
class CheckSpec:
    group: str
    maximum: int
    kind: str


_ROWS = (
    ("readme.exists", "required_files", 5, "DETERMINISTIC"),
    ("license.exists", "required_files", 5, "DETERMINISTIC"),
    ("contributing.exists", "required_files", 5, "DETERMINISTIC"),
    ("codeowners.exists", "required_files", 5, "DETERMINISTIC"),
    ("purpose.summary", "purpose", 8, "SEMANTIC"),
    ("purpose.capabilities", "purpose", 7, "SEMANTIC"),
    ("local_run.prerequisites", "local_run", 3, "SEMANTIC"),
    ("local_run.dependencies", "local_run", 4, "SEMANTIC"),
    ("local_run.configuration", "local_run", 4, "SEMANTIC"),
    ("local_run.execution", "local_run", 6, "SEMANTIC"),
    ("local_run.verification", "local_run", 3, "SEMANTIC"),
    ("build.commands", "build", 6, "SEMANTIC"),
    ("build.configuration", "build", 2, "SEMANTIC"),
    ("build.output", "build", 2, "SEMANTIC"),
    ("testing.commands", "testing", 5, "SEMANTIC"),
    ("testing.environment", "testing", 3, "SEMANTIC"),
    ("testing.scope", "testing", 2, "SEMANTIC"),
    ("usage.primary_example", "usage", 5, "SEMANTIC"),
    ("usage.inputs", "usage", 3, "SEMANTIC"),
    ("usage.expected_result", "usage", 2, "SEMANTIC"),
    ("structure.overview", "structure", 3, "SEMANTIC"),
    ("structure.navigation", "structure", 2, "SEMANTIC"),
    ("consistency.references", "consistency", 3, "HYBRID"),
    ("consistency.commands", "consistency", 4, "HYBRID"),
    ("consistency.description", "consistency", 3, "HYBRID"),
)

CHECKS = {name: CheckSpec(group, maximum, kind) for name, group, maximum, kind in _ROWS}
GROUPS = tuple(dict.fromkeys(spec.group for spec in CHECKS.values()))

# Component IDs and maxima are part of documentation-grading-v1. Only ordinary
# code sums these integer awards; the model cannot set a check total on its own.
COMPONENTS = {
    "purpose.summary": {"project_identity": 1, "problem_statement": 2, "solution_role": 2,
                        "expected_outcome": 2, "summary_clarity": 1},
    "purpose.capabilities": {"capability_coverage": 4, "capability_clarity": 3},
    "local_run.prerequisites": {"required_tools": 1, "required_versions": 1,
                                "external_requirements": 1},
    "local_run.dependencies": {"installation_command": 2, "installation_context": 1,
                               "dependency_variants": 1},
    "local_run.configuration": {"required_parameters": 2, "configuration_steps": 1,
                                "secret_handling": 1},
    "local_run.execution": {"execution_commands": 3, "execution_sequence": 2,
                            "runtime_entrypoint": 1},
    "local_run.verification": {"success_indicator": 2, "smoke_check": 1},
    "build.commands": {"primary_command": 3, "build_sequence": 2, "required_targets": 1},
    "build.configuration": {"build_parameters": 1, "build_profiles": 1},
    "build.output": {"artifact_identity": 1, "artifact_location": 1},
    "testing.commands": {"primary_test_command": 3, "command_context": 1, "test_variants": 1},
    "testing.environment": {"test_services": 1, "test_configuration": 1, "test_fixtures": 1},
    "testing.scope": {"test_types": 1, "command_scope": 1},
    "usage.primary_example": {"executable_example": 3, "primary_scenario": 1,
                              "usage_context": 1},
    "usage.inputs": {"required_inputs": 2, "input_format": 1},
    "usage.expected_result": {"result_form": 1, "result_example": 1},
    "structure.overview": {"component_map": 2, "component_roles": 1},
    "structure.navigation": {"important_documents": 1, "section_navigation": 1},
    "consistency.references": {"file_targets": 2, "link_targets": 1},
    "consistency.commands": {"command_targets": 2, "command_parameters": 1,
                             "configuration_references": 1},
    "consistency.description": {"described_components": 1, "described_capabilities": 1,
                                "described_technologies": 1},
}

_FILE_RULES = {
    "readme.exists": ("README", ("", ".md", ".rst", ".adoc"), ("",), "DOC_README"),
    "license.exists": ("LICENSE", ("", ".md", ".txt"), ("",), "DOC_LICENSE"),
    "contributing.exists": (
        "CONTRIBUTING", ("", ".md", ".rst", ".adoc"),
        ("", ".github/", "docs/"), "DOC_CONTRIBUTING",
    ),
    "codeowners.exists": ("CODEOWNERS", ("",), ("", ".github/", "docs/"), "DOC_CODEOWNERS"),
}


def required_file_paths(all_paths: set[str]) -> set[str]:
    """Select the objective-check documents from a complete S3 object listing."""
    folded = {path.lower(): path for path in all_paths}
    if len(folded) != len(all_paths):
        raise ValueError("case-insensitive duplicate paths")
    selected = set()
    for check_id, (stem, extensions, dirs, _) in _FILE_RULES.items():
        names = [f"{directory}{stem}{ext}" for directory in dirs for ext in extensions]
        if check_id == "license.exists":
            names += [name.replace("LICENSE", "LICENCE") for name in names]
        found = next((folded[name.lower()] for name in names if name.lower() in folded), None)
        if found is not None:
            selected.add(found)
    return selected


def _path(path: str) -> str:
    if not isinstance(path, str) or not path or "\\" in path:
        raise ValueError("invalid repository path")
    parts = PurePosixPath(path).parts
    if path.startswith("/") or any(part in ("", ".", "..") for part in path.split("/")):
        raise ValueError("repository path must be relative and normalized")
    if not parts or path.startswith("_meta/"):
        raise ValueError("path is outside repository documentation")
    return path


def _empty_reason(content: str) -> str | None:
    if not content:
        return "EMPTY"
    if not content.strip():
        return "WHITESPACE_ONLY"
    meaningful = []
    for line in content.splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith(("# ", "## ", "### ")):
            # A heading alone is not documentation, but keep checking later lines.
            continue
        if stripped.startswith(("//", "<!--", "-->", ";")):
            continue
        meaningful.append(stripped)
    if meaningful:
        return None
    if any(line.lstrip().startswith("#") for line in content.splitlines()):
        return "TITLE_ONLY"
    return "COMMENTS_ONLY"


def deterministic_checks(files: Mapping[str, str], all_paths: set[str] | None = None) -> dict[str, dict]:
    """Evaluate the four objective checks against a complete repository listing.

    ``all_paths`` is the complete object listing below the S3 prefix; ``files``
    contains complete text for selected files. Every discovered required file
    must be selected. The caller proves listing/content stability separately.
    """
    for path, content in files.items():
        _path(path)
        if not isinstance(content, str):
            raise ValueError("file contents must be text")
    all_paths = set(files) if all_paths is None else set(all_paths)
    if not set(files) <= all_paths:
        raise ValueError("selected file absent from listing")
    for path in all_paths:
        _path(path)
    folded = {path.lower(): path for path in all_paths}
    if len(folded) != len(all_paths):
        raise ValueError("case-insensitive duplicate paths")

    result = {}
    for check_id, (stem, extensions, dirs, code_prefix) in _FILE_RULES.items():
        names = [f"{directory}{stem}{ext}" for directory in dirs for ext in extensions]
        if check_id == "license.exists":
            names += [name.replace("LICENSE", "LICENCE") for name in names]
        found = next((folded[name.lower()] for name in names if name.lower() in folded), None)
        deductions = []
        if found is None:
            deductions.append({
                "deduction_id": f"{check_id}:missing",
                "code": f"{code_prefix}_MISSING",
                "raw_points_lost": 5,
                "facts": {"searched_paths": names},
                "evidence": [{"type": "file_missing", "path": names[0]}],
            })
        else:
            if found not in files:
                raise ValueError(f"required file content not selected: {found}")
            reason = _empty_reason(files[found])
            if reason is not None:
                deductions.append({
                    "deduction_id": f"{check_id}:empty",
                    "code": f"{code_prefix}_EMPTY",
                    "raw_points_lost": 5,
                    "facts": {"path": found, "empty_reason": reason},
                    "evidence": [{"type": "file_exists", "path": found}],
                })
        result[check_id] = {
            "check_id": check_id,
            "check_type": "DETERMINISTIC",
            "outcome": "EVALUATED",
            "points_awarded": 0 if deductions else 5,
            "max_points": 5,
            "deductions": deductions,
        }
    return result


def _rounded(numerator: int, denominator: int) -> float:
    return float((Decimal(numerator) * 100 / Decimal(denominator)).quantize(
        Decimal("0.01"), rounding=ROUND_HALF_UP
    ))


def _problems(checks: list[dict], evaluated: int, score: float) -> list[dict]:
    deductions = [(check["check_id"], deduction) for check in checks
                  for deduction in check["deductions"]]
    remaining = (Decimal(100) - Decimal(str(score))).quantize(Decimal("0.01"))
    result = []
    for index, (check_id, deduction) in enumerate(deductions):
        impact = (remaining if index == len(deductions) - 1 else
                  (Decimal(deduction["raw_points_lost"]) * 100 / Decimal(evaluated)).quantize(
                      Decimal("0.01"), rounding=ROUND_HALF_UP))
        remaining -= impact
        if impact <= 0:
            raise ValueError("deduction has no normalized score impact")
        severity = ("CRITICAL" if impact > 50 else "HIGH" if impact > 25 else
                    "MEDIUM" if impact > 10 else "LOW")
        if deduction["code"] in {"DOC_README_MISSING", "DOC_README_EMPTY",
                                 "DOC_LICENSE_MISSING", "DOC_LICENSE_EMPTY"}:
            if severity == "LOW" or severity == "MEDIUM":
                severity = "HIGH"
        result.append({
            "id": "DOC-" + sha256(deduction["deduction_id"].encode()).hexdigest()[:12].upper(),
            "category": "documentation",
            "code": deduction["code"],
            "severity": severity,
            "score_impact": {"category_points_lost": float(impact)},
            "facts": deduction["facts"],
            "evidence": deduction["evidence"],
            "source_checks": [check_id],
            "source_deductions": [deduction["deduction_id"]],
        })
    return result


def _validate_evidence(evidence: object, files: Mapping[str, str], all_paths: set[str],
                       context_sha256: str,
                       reviewed_paths: set[str]) -> None:
    if not isinstance(evidence, list) or not evidence:
        raise ValueError("each deduction needs verified evidence")
    for item in evidence:
        if not isinstance(item, dict):
            raise ValueError("invalid evidence")
        kind = item.get("type")
        if kind in ("file_exists", "file_missing", "file_content"):
            expected_fields = ({"type", "path", "start_line", "end_line", "quote"}
                               if kind == "file_content" else {"type", "path"})
            if set(item) != expected_fields:
                raise ValueError("evidence contains unknown or missing fields")
            path = _path(item.get("path"))
            if (path in all_paths) != (kind != "file_missing"):
                raise ValueError("evidence does not match repository listing")
            if kind == "file_content":
                if path not in files:
                    raise ValueError("quoted file content was not selected")
                start, end, quote = item.get("start_line"), item.get("end_line"), item.get("quote")
                lines = files[path].splitlines()
                if (type(start) is not int or type(end) is not int or start < 1 or
                        end < start or end > len(lines) or not isinstance(quote, str) or
                        not quote or quote not in "\n".join(lines[start - 1:end])):
                    raise ValueError("file quote or line range is not verified")
        elif kind == "document_review":
            if set(item) != {"type", "paths", "context_sha256"}:
                raise ValueError("document review contains unknown or missing fields")
            paths = item.get("paths")
            if (item.get("context_sha256") != context_sha256 or
                    not isinstance(paths, list) or not paths or
                    any(not isinstance(path, str) for path in paths) or
                    len(set(paths)) != len(paths) or
                    any(_path(path) not in reviewed_paths or path not in files for path in paths)):
                raise ValueError("document review is outside the complete model context")
        else:
            raise ValueError("unsupported evidence type")


def grade_documentation(files: Mapping[str, str], semantic_checks: Mapping[str, dict], *,
                        context_sha256: str, reviewed_paths: set[str],
                        all_paths: set[str] | None = None,
                        trusted_facts_by_deduction: Mapping[str, dict] | None = None) -> dict:
    """Validate all model/hybrid checks and calculate contract-compatible scores.

    The orchestration layer must supply a stable S3 snapshot and the hash of the
    exact complete context sent to the model. This core validates registered
    codes, typed facts, file evidence, and point arithmetic.
    """
    from .deductions import validate_deduction

    trusted_facts_by_deduction = trusted_facts_by_deduction or {}
    all_paths = set(files) if all_paths is None else set(all_paths)
    objective = deterministic_checks(files, all_paths)
    expected = set(CHECKS) - set(objective)
    if set(semantic_checks) != expected:
        raise ValueError(f"semantic checks differ: missing={expected - set(semantic_checks)}, "
                         f"extra={set(semantic_checks) - expected}")
    if not isinstance(context_sha256, str) or len(context_sha256) != 64:
        raise ValueError("invalid context hash")
    if any(_path(path) not in files for path in reviewed_paths):
        raise ValueError("reviewed file missing")

    checks = []
    seen_deductions = set()
    for check_id, spec in CHECKS.items():
        check = objective.get(check_id, semantic_checks.get(check_id))
        if not isinstance(check, dict) or check.get("check_id") != check_id:
            raise ValueError(f"invalid check: {check_id}")
        expected_fields = {"check_id", "check_type", "outcome", "points_awarded",
                           "max_points", "deductions"}
        if check_id in COMPONENTS:
            expected_fields.add("components")
        if set(check) != expected_fields:
            raise ValueError(f"unknown or missing check fields: {check_id}")
        if check.get("check_type") != spec.kind or check.get("max_points") != spec.maximum:
            raise ValueError(f"check method mismatch: {check_id}")
        outcome, awarded, deductions = (check.get("outcome"), check.get("points_awarded"),
                                         check.get("deductions"))
        if check_id in COMPONENTS:
            components = check.get("components")
            if outcome == "EVALUATED":
                if not isinstance(components, dict) or set(components) != set(COMPONENTS[check_id]):
                    raise ValueError(f"invalid components: {check_id}")
                if any(type(value) is not int or not 0 <= value <= COMPONENTS[check_id][name]
                       for name, value in components.items()):
                    raise ValueError(f"component points out of bounds: {check_id}")
                if sum(components.values()) != awarded:
                    raise ValueError(f"component sum differs from points: {check_id}")
            elif components not in (None, {}):
                raise ValueError(f"non-evaluated check has component points: {check_id}")
        if not isinstance(deductions, list):
            raise ValueError("deductions must be a list")
        if outcome == "EVALUATED":
            if type(awarded) is not int or not 0 <= awarded <= spec.maximum:
                raise ValueError(f"points out of bounds: {check_id}")
        elif outcome in ("NO_DATA", "NOT_APPLICABLE"):
            if awarded is not None or deductions:
                raise ValueError("non-evaluated check cannot deduct points")
        else:
            raise ValueError(f"unknown outcome: {outcome}")
        lost = 0
        for deduction in deductions:
            if not isinstance(deduction, dict):
                raise ValueError("invalid deduction")
            deduction_id = deduction.get("deduction_id")
            amount = deduction.get("raw_points_lost")
            if (not isinstance(deduction_id, str) or not deduction_id.startswith(check_id + ":") or
                    deduction_id in seen_deductions or type(amount) is not int or amount <= 0 or
                    not isinstance(deduction.get("code"), str) or
                    not deduction["code"].startswith("DOC_") or
                    not isinstance(deduction.get("facts"), dict)):
                raise ValueError(f"invalid deduction for {check_id}")
            seen_deductions.add(deduction_id)
            lost += amount
            _validate_evidence(deduction.get("evidence"), files, all_paths, context_sha256,
                               reviewed_paths)
            validate_deduction(check_id, deduction, reviewed_paths, all_paths,
                               trusted_facts_by_deduction.get(deduction_id))
        if outcome == "EVALUATED" and lost != spec.maximum - awarded:
            raise ValueError(f"deductions do not explain score: {check_id}")
        checks.append(check)

    applicable = sum(c["max_points"] for c in checks if c["outcome"] != "NOT_APPLICABLE")
    evaluated = sum(c["max_points"] for c in checks if c["outcome"] == "EVALUATED")
    earned = sum(c["points_awarded"] for c in checks if c["outcome"] == "EVALUATED")
    if not applicable or not evaluated:
        raise ValueError("no evaluated Documentation points")
    score = _rounded(earned, evaluated)
    groups = []
    for group in GROUPS:
        members = [c for c in checks if CHECKS[c["check_id"]].group == group]
        group_applicable = sum(c["max_points"] for c in members if c["outcome"] != "NOT_APPLICABLE")
        group_evaluated = sum(c["max_points"] for c in members if c["outcome"] == "EVALUATED")
        group_earned = sum(c["points_awarded"] for c in members if c["outcome"] == "EVALUATED")
        status = ("NOT_APPLICABLE" if not group_applicable else "NO_DATA" if not group_evaluated
                  else "CALCULATED" if group_applicable == group_evaluated else "CALCULATED_PARTIAL")
        groups.append({"id": group, "status": status,
                       "score": _rounded(group_earned, group_evaluated) if group_evaluated else None,
                       "applicable_points": group_applicable, "evaluated_points": group_evaluated,
                       "earned_points": group_earned})

    return {
        "category": "documentation",
        "grading_method_version": METHOD_VERSION,
        "score": {"status": "CALCULATED" if applicable == evaluated else "CALCULATED_PARTIAL",
                  "value": score, "scale": 100},
        "coverage": {"applicable_points": applicable, "evaluated_points": evaluated,
                     "percent": _rounded(evaluated, applicable)},
        "subcategories": groups,
        "checks": checks,
        "problems": _problems(checks, evaluated, score),
        "warnings": [],
    }
