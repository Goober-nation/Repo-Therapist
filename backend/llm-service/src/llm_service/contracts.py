"""Versioned Kafka request and compact Documentation result formats."""

from __future__ import annotations

import json
import re
from datetime import datetime, timezone
from decimal import Decimal
from uuid import NAMESPACE_URL, UUID, uuid5

from .grading import GROUPS

EVENT_VERSION = 1
DOCUMENTATION_RESULT_VERSION = 2
REPORT_REQUEST_VERSION = 2
CATEGORIES = frozenset({"security", "activity", "documentation", "ci_cd", "issues", "code_health"})
SCORED = frozenset({"CALCULATED", "CALCULATED_PARTIAL"})
UNSCORED = frozenset({"NO_DATA", "NOT_CALCULATED", "FAILED"})


class InvalidEvent(ValueError):
    pass


class UnsupportedEventVersion(InvalidEvent):
    pass


def _unique_pairs(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise InvalidEvent(f"duplicate JSON field: {key}")
        result[key] = value
    return result


def _reject_constant(value):
    raise InvalidEvent("non-finite JSON number")


def _uuid(value):
    if not isinstance(value, str):
        raise InvalidEvent("UUID field is not a string")
    try:
        parsed = UUID(value)
    except ValueError as exc:
        raise InvalidEvent("invalid UUID") from exc
    if str(parsed) != value:
        raise InvalidEvent("UUID must use canonical lowercase form")


def _number(value):
    return type(value) in (int, float) and 0 <= value <= 100


def _score(item, *, overall=False):
    if not isinstance(item, dict):
        raise InvalidEvent("invalid score object")
    status = item.get("status")
    value = item.get("score")
    if status in SCORED:
        if not _number(value):
            raise InvalidEvent("calculated score must be 0..100")
    elif status in UNSCORED:
        if value is not None:
            raise InvalidEvent("unavailable score must be null")
    else:
        raise InvalidEvent("unknown score status")
    if not overall and status == "CALCULATED_PARTIAL":
        coverage = item.get("coverage_percent")
        if not _number(coverage):
            raise InvalidEvent("partial score requires coverage_percent")


def validate_documentation_payload(payload):
    """Validate only the compact wire format, never facts or deduction arithmetic."""
    if not isinstance(payload, dict) or set(payload) != {"grade", "subcategories", "facts"}:
        raise InvalidEvent("documentation payload requires only grade, subcategories, facts")
    if not _number(payload["grade"]):
        raise InvalidEvent("documentation grade must be 0..100")
    groups = payload["subcategories"]
    if not isinstance(groups, list) or len(groups) != len(GROUPS):
        raise InvalidEvent("all eight documentation subcategories are required")
    seen = set()
    for group in groups:
        if not isinstance(group, dict) or set(group) != {"id", "grade"}:
            raise InvalidEvent("subcategory requires only id and grade")
        identifier = group["id"]
        if not isinstance(identifier, str) or identifier not in GROUPS or identifier in seen:
            raise InvalidEvent("unknown or duplicate documentation subcategory")
        seen.add(identifier)
        if group["grade"] is not None and not _number(group["grade"]):
            raise InvalidEvent("subcategory grade must be 0..100 or null")
    if not isinstance(payload["facts"], list):
        raise InvalidEvent("documentation facts must be an array")
    ids = set()
    for fact in payload["facts"]:
        if not isinstance(fact, dict) or set(fact) != {"id", "subcategory", "code", "facts", "evidence"}:
            raise InvalidEvent("invalid documentation fact fields")
        identifier = fact["id"]
        if not isinstance(identifier, str) or not identifier or identifier in ids:
            raise InvalidEvent("invalid or duplicate documentation fact id")
        ids.add(identifier)
        if (not isinstance(fact["subcategory"], str) or fact["subcategory"] not in GROUPS or
                not isinstance(fact["code"], str) or not isinstance(fact["facts"], dict) or
                not isinstance(fact["evidence"], list) or
                any(not isinstance(item, dict) for item in fact["evidence"])):
            raise InvalidEvent("invalid documentation fact types")


def _report_payload(payload, version=1):
    if payload.get("output_language") != "ru":
        raise InvalidEvent("report language must be ru")
    scores = payload.get("scores")
    if not isinstance(scores, dict):
        raise InvalidEvent("missing report scores")
    _score(scores.get("overall"), overall=True)
    categories = scores.get("categories")
    if not isinstance(categories, list) or len(categories) != len(CATEGORIES):
        raise InvalidEvent("all six category scores are required")
    by_category = {}
    for item in categories:
        _score(item)
        category = item.get("category")
        if category not in CATEGORIES or category in by_category:
            raise InvalidEvent("unknown or duplicate category")
        by_category[category] = item
    if version == REPORT_REQUEST_VERSION:
        if "documentation" not in payload:
            raise InvalidEvent("report v2 requires documentation payload or null")
        documentation = payload["documentation"]
        if by_category["documentation"]["status"] in SCORED:
            validate_documentation_payload(documentation)
        elif documentation is not None:
            raise InvalidEvent("unavailable documentation must be null")
    problems = payload.get("problems")
    if not isinstance(problems, list):
        raise InvalidEvent("report problems must be a list")
    sums = {name: Decimal(0) for name in CATEGORIES}
    ids = set()
    for problem in problems:
        if not isinstance(problem, dict):
            raise InvalidEvent("invalid problem")
        identifier, category = problem.get("id"), problem.get("category")
        impact = problem.get("score_impact")
        if (not isinstance(identifier, str) or not identifier or identifier in ids or
                category not in CATEGORIES or problem.get("severity") not in
                {"LOW", "MEDIUM", "HIGH", "CRITICAL"} or
                not isinstance(problem.get("code"), str) or
                not isinstance(problem.get("facts"), dict) or
                not isinstance(problem.get("evidence"), list) or not problem["evidence"] or
                not isinstance(impact, dict)):
            raise InvalidEvent("invalid report problem")
        ids.add(identifier)
        if version == REPORT_REQUEST_VERSION and category == "documentation":
            raise InvalidEvent("documentation facts belong in documentation, not problems")
        lost = impact.get("category_points_lost")
        if not _number(lost) or lost == 0:
            raise InvalidEvent("problem impact must be positive")
        sums[category] += Decimal(str(lost))
    for category, score in by_category.items():
        if version == REPORT_REQUEST_VERSION and category == "documentation":
            continue
        if score["status"] in SCORED:
            expected = Decimal(100) - Decimal(str(score["score"]))
            if abs(sums[category] - expected) > Decimal("0.01"):
                raise InvalidEvent(f"problem impacts do not explain {category} score")
        elif sums[category]:
            raise InvalidEvent("unavailable category has scoring problems")


def _parse_envelope(raw: bytes) -> dict:
    if not isinstance(raw, bytes) or len(raw) > 1_048_576:
        raise InvalidEvent("Kafka record is not bytes or is too large")
    try:
        event = json.loads(raw.decode("utf-8"), object_pairs_hook=_unique_pairs,
                           parse_constant=_reject_constant)
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise InvalidEvent("request is not valid UTF-8 JSON") from exc
    if not isinstance(event, dict):
        raise InvalidEvent("event envelope must be an object")
    if type(event.get("event_version")) is not int:
        raise UnsupportedEventVersion("unsupported event version")
    for field in ("event_id", "analysis_id", "repository_id"):
        _uuid(event.get(field))
    if event.get("data_scope") not in {"public", "owner"}:
        raise InvalidEvent("invalid data scope")
    timestamp = event.get("created_at")
    if not isinstance(timestamp, str) or not timestamp.endswith("Z"):
        raise InvalidEvent("created_at must be UTC RFC 3339 with Z")
    try:
        if datetime.fromisoformat(timestamp.replace("Z", "+00:00")).tzinfo != timezone.utc:
            raise InvalidEvent("created_at must be UTC")
    except ValueError as exc:
        raise InvalidEvent("invalid created_at") from exc
    commit_sha = event.get("commit_sha")
    if commit_sha is not None and (not isinstance(commit_sha, str) or
                                   not re.fullmatch(r"[0-9a-f]{40}", commit_sha)):
        raise InvalidEvent("invalid commit_sha")
    payload = event.get("payload")
    if not isinstance(payload, dict):
        raise InvalidEvent("payload must be an object")
    return event


def parse_request(raw: bytes) -> dict:
    """Documentation requests stay v1; reports accept legacy v1 and compact-input v2."""
    event = _parse_envelope(raw)
    event_type, payload = event.get("event_type"), event["payload"]
    if event_type not in {"documentation.analysis.requested", "report.generation.requested"}:
        raise InvalidEvent("unknown request event type")
    versions = {1} if event_type == "documentation.analysis.requested" else {1, REPORT_REQUEST_VERSION}
    if event["event_version"] not in versions:
        raise UnsupportedEventVersion("unsupported event version")
    if event_type == "documentation.analysis.requested":
        prefix = payload.get("s3_prefix")
        if (not isinstance(prefix, str) or
                prefix != f"repos/{event['repository_id']}"):
            raise InvalidEvent("s3_prefix does not match repository_id")
    else:
        if len(raw) > 524_288:
            raise InvalidEvent("report request exceeds 512 KiB")
        _report_payload(payload, event["event_version"])
    return event


def parse_documentation_result(raw: bytes) -> dict:
    event = _parse_envelope(raw)
    if event.get("event_type") != "documentation.analysis.completed":
        raise InvalidEvent("expected documentation.analysis.completed")
    if event["event_version"] != DOCUMENTATION_RESULT_VERSION:
        raise UnsupportedEventVersion("expected documentation result v2")
    if len(raw) > 524_288:
        raise InvalidEvent("documentation result exceeds 512 KiB")
    _uuid(event.get("request_event_id"))
    validate_documentation_payload(event["payload"])
    return event


def build_documentation_completed(request: dict, payload: dict) -> dict:
    request = parse_request(json.dumps(request, allow_nan=False).encode("utf-8"))
    if request["event_type"] != "documentation.analysis.requested":
        raise InvalidEvent("expected documentation request")
    event = {key: request[key] for key in ("analysis_id", "repository_id", "data_scope")}
    if "commit_sha" in request:
        event["commit_sha"] = request["commit_sha"]
    event.update(event_id=_documentation_terminal_id(request), event_type="documentation.analysis.completed",
                 event_version=DOCUMENTATION_RESULT_VERSION, request_event_id=request["event_id"],
                 created_at=datetime.now(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z"),
                 payload=payload)
    return parse_documentation_result(json.dumps(event, ensure_ascii=False, allow_nan=False).encode("utf-8"))


def build_documentation_failed(request: dict, code: str, retryable: bool, message: str) -> dict:
    """Build a correlated failure even when the request payload/version is invalid."""
    request = _parse_envelope(json.dumps(request, allow_nan=False).encode("utf-8"))
    if request.get("event_type") != "documentation.analysis.requested":
        raise InvalidEvent("expected documentation request")
    if code not in {
        "INVALID_EVENT", "UNSUPPORTED_EVENT_VERSION", "S3_PREFIX_NOT_FOUND",
        "S3_CONTENT_CHANGED", "S3_UNAVAILABLE", "CONTEXT_BUILD_FAILED",
        "LLM_PROVIDER_UNAVAILABLE", "LLM_RATE_LIMITED", "LLM_INVALID_RESPONSE",
        "INTERNAL_ERROR",
    } or type(retryable) is not bool or not isinstance(message, str) or not message:
        raise InvalidEvent("invalid documentation failure")
    event = {key: request[key] for key in ("analysis_id", "repository_id", "data_scope")}
    if "commit_sha" in request:
        event["commit_sha"] = request["commit_sha"]
    event.update(event_id=_documentation_terminal_id(request), event_type="documentation.analysis.failed",
                 event_version=1, request_event_id=request["event_id"],
                 created_at=datetime.now(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z"),
                 payload={"error": {"code": code, "retryable": retryable, "message": message}})
    return event


def _documentation_terminal_id(request: dict) -> str:
    """Keep terminal event identity stable when Kafka redelivers a request."""
    return str(uuid5(NAMESPACE_URL, f"sourcecraft:documentation:{request['event_id']}"))
