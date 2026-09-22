"""Validate the v1 Kafka requests before any model or S3 work begins."""

from __future__ import annotations

import json
import re
from datetime import datetime, timezone
from decimal import Decimal
from uuid import UUID

EVENT_VERSION = 1
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


def _report_payload(payload):
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
        lost = impact.get("category_points_lost")
        if not _number(lost) or lost == 0:
            raise InvalidEvent("problem impact must be positive")
        sums[category] += Decimal(str(lost))
    for category, score in by_category.items():
        if score["status"] in SCORED:
            expected = Decimal(100) - Decimal(str(score["score"]))
            if abs(sums[category] - expected) > Decimal("0.01"):
                raise InvalidEvent(f"problem impacts do not explain {category} score")
        elif sums[category]:
            raise InvalidEvent("unavailable category has scoring problems")


def parse_request(raw: bytes) -> dict:
    """Decode a Documentation or report request according to kafka-contracts v1."""
    if not isinstance(raw, bytes) or len(raw) > 1_048_576:
        raise InvalidEvent("Kafka record is not bytes or is too large")
    try:
        event = json.loads(raw.decode("utf-8"), object_pairs_hook=_unique_pairs)
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise InvalidEvent("request is not valid UTF-8 JSON") from exc
    if not isinstance(event, dict):
        raise InvalidEvent("event envelope must be an object")
    if event.get("event_version") != EVENT_VERSION or type(event.get("event_version")) is not int:
        raise UnsupportedEventVersion("unsupported event version")
    event_type = event.get("event_type")
    if event_type not in {"documentation.analysis.requested", "report.generation.requested"}:
        raise InvalidEvent("unknown request event type")
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
    if event_type == "documentation.analysis.requested":
        prefix = payload.get("s3_prefix")
        if (not isinstance(prefix, str) or
                prefix != f"repos/{event['repository_id']}"):
            raise InvalidEvent("s3_prefix does not match repository_id")
    else:
        if len(raw) > 524_288:
            raise InvalidEvent("report request exceeds 512 KiB")
        _report_payload(payload)
    return event
