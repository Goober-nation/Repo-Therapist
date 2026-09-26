"""Format-only boundary for model grading: no semantic or evidence verification."""

import json
from decimal import Decimal, ROUND_HALF_UP

from .context import ModelContext
from .grading import CHECKS, COMPONENTS, GROUPS, METHOD_VERSION, deterministic_checks
from .model_response import InvalidModelResponse
from .snapshot import Snapshot


def _object(value, keys, location):
    if not isinstance(value, dict) or set(value) != set(keys):
        raise InvalidModelResponse(f"{location}: expected fields {', '.join(keys)}")


def _integer(value, location):
    if type(value) is not int:
        raise InvalidModelResponse(f"{location}: expected integer")


def _string(value, location):
    if not isinstance(value, str):
        raise InvalidModelResponse(f"{location}: expected string")


def _pairs(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise InvalidModelResponse("duplicate JSON field")
        result[key] = value
    return result


def _constant(value):
    raise InvalidModelResponse("non-finite JSON number")


def _evidence(item):
    if not isinstance(item, dict):
        raise InvalidModelResponse("evidence item: expected object")
    kind = item.get("type")
    if kind in ("file_content", "file_exists", "file_missing"):
        keys = ["type", "path"]
        if kind == "file_content":
            keys += ["start_line", "end_line", "quote"]
        _object(item, keys, "evidence")
        _string(item["path"], "evidence.path")
        if kind == "file_content":
            _integer(item["start_line"], "evidence.start_line")
            _integer(item["end_line"], "evidence.end_line")
            _string(item["quote"], "evidence.quote")
    elif kind == "document_review":
        _object(item, ["type", "paths", "context_sha256"], "evidence")
        _string(item["context_sha256"], "evidence.context_sha256")
        if not isinstance(item["paths"], list):
            raise InvalidModelResponse("evidence.paths: expected array")
        for path in item["paths"]:
            _string(path, "evidence.paths item")
    else:
        raise InvalidModelResponse("evidence.type: unknown variant")


def _percent(earned, maximum):
    return float((Decimal(earned) * 100 / Decimal(maximum)).quantize(
        Decimal("0.01"), rounding=ROUND_HALF_UP)) if maximum else None


def parse_formatted_response(raw: bytes, snapshot: Snapshot, context: ModelContext) -> dict:
    """Preserve model claims, including inconsistent arithmetic and unverified quotes.

    Scores displayed in the summary use the model's points_awarded, not deductions
    or component sums. This result is not a verified Kafka result envelope.
    """
    if not isinstance(raw, bytes) or len(raw) > 512_000:
        raise InvalidModelResponse("response must be UTF-8 JSON within 512000 bytes")
    try:
        body = json.loads(raw.decode("utf-8"), object_pairs_hook=_pairs,
                          parse_constant=_constant)
    except (UnicodeDecodeError, json.JSONDecodeError, RecursionError) as exc:
        raise InvalidModelResponse("response is not valid UTF-8 JSON") from exc
    _object(body, ["checks"], "response")
    if not isinstance(body["checks"], list):
        raise InvalidModelResponse("checks: expected array")
    by_id = {}
    for check in body["checks"]:
        _object(check, ["check_id", "check_type", "outcome", "points_awarded",
                        "max_points", "components", "deductions"], "check")
        check_id = check["check_id"]
        if not isinstance(check_id, str) or check_id not in COMPONENTS or check_id in by_id:
            raise InvalidModelResponse("check_id: unknown or duplicate ID")
        spec = CHECKS[check_id]
        _integer(check["max_points"], "max_points")
        if check["check_type"] != spec.kind or check["max_points"] != spec.maximum:
            raise InvalidModelResponse(f"{check_id}: wrong check_type or max_points")
        if check["outcome"] == "EVALUATED":
            _integer(check["points_awarded"], "points_awarded")
            if not 0 <= check["points_awarded"] <= spec.maximum:
                raise InvalidModelResponse(f"{check_id}: points_awarded outside range")
            _object(check["components"], list(COMPONENTS[check_id]), "components")
            for name, value in check["components"].items():
                _integer(value, f"components.{name}")
                if not 0 <= value <= COMPONENTS[check_id][name]:
                    raise InvalidModelResponse(f"components.{name}: outside range")
        elif check["outcome"] == "NOT_APPLICABLE":
            if check["points_awarded"] is not None or check["components"] is not None:
                raise InvalidModelResponse("NOT_APPLICABLE requires null scores")
        else:
            raise InvalidModelResponse("outcome: unknown variant")
        if not isinstance(check["deductions"], list):
            raise InvalidModelResponse("deductions: expected array")
        for deduction in check["deductions"]:
            _object(deduction, ["deduction_id", "code", "raw_points_lost", "facts", "evidence"],
                    "deduction")
            _string(deduction["deduction_id"], "deduction_id")
            _string(deduction["code"], "code")
            _integer(deduction["raw_points_lost"], "raw_points_lost")
            if not isinstance(deduction["facts"], dict):
                raise InvalidModelResponse("facts: expected object")
            if not isinstance(deduction["evidence"], list):
                raise InvalidModelResponse("evidence: expected array")
            for item in deduction["evidence"]:
                _evidence(item)
        by_id[check_id] = check
    if set(by_id) != set(COMPONENTS):
        raise InvalidModelResponse("missing model checks: expected all 21 IDs")

    coverage = json.loads(context.text).get("coverage", {})
    if coverage:
        for name, check in by_id.items():
            if coverage.get(name) == "not_reviewed":
                check["outcome"] = "NOT_REVIEWED"
                check["points_awarded"] = None
                check["components"] = None
                check["deductions"] = []
    by_id.update(deterministic_checks(snapshot.files, set(snapshot.all_paths)))
    checks = [by_id[name] for name in CHECKS]
    subcategories = []
    for group in GROUPS:
        members = [c for c in checks if CHECKS[c["check_id"]].group == group
                   and c["outcome"] == "EVALUATED"]
        maximum = sum(c["max_points"] for c in members)
        earned = sum(c["points_awarded"] for c in members)
        subcategories.append({"id": group, "status": "CALCULATED" if maximum else "NOT_REVIEWED",
                              "score": _percent(earned, maximum)})
    evaluated = [c for c in checks if c["outcome"] == "EVALUATED"]
    score = _percent(sum(c["points_awarded"] for c in evaluated),
                     sum(c["max_points"] for c in evaluated))
    return {"category": "documentation", "grading_method_version": METHOD_VERSION,
            "validation": "format_only", "score": {"status": "CALCULATED", "value": score,
                                                     "scale": 100},
            "subcategories": subcategories, "checks": checks,
            "context": {"sha256": context.sha256, "selected_file_count": context.selected_file_count,
                        "total_file_count": context.total_file_count, "truncated": context.truncated}}
