"""Aggregator <-> LLM flat Kafka format for repository summaries and recommendations."""
import json
import re
from datetime import datetime, timezone

from .contracts import InvalidEvent, _unique_pairs, _reject_constant, _uuid

REQUEST_TOPIC = "llm.recommendations.requested"
RESULT_TOPIC = "llm.recommendations.received"
VISIBILITIES = frozenset({"public", "private"})

_MISSING = object()
_TIMESTAMP = re.compile(r"(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(\.\d+)?(Z|[+-]\d{2}:\d{2})")


def _key(name):
    return name.replace("_", "").lower()


def _field(obj, name):
    """Read a field by snake_case or Go field name; RepoGrade has no json tags."""
    matches = [key for key in obj if isinstance(key, str) and _key(key) == _key(name)]
    if len(matches) > 1:
        raise InvalidEvent(f"field {name} is present in several spellings")
    return obj[matches[0]] if matches else _MISSING


def _timestamp(value, name):
    match = _TIMESTAMP.fullmatch(value) if isinstance(value, str) else None
    if match is None:
        raise InvalidEvent(f"{name} must be RFC3339")
    # Go emits nanoseconds; Python before 3.12 parses at most microseconds.
    fraction = (match[2] or "")[:7]
    parsed = datetime.fromisoformat(match[1] + fraction + match[3].replace("Z", "+00:00"))
    if parsed.tzinfo is None:
        raise InvalidEvent(f"{name} must include a timezone")


def _score(value, name):
    if isinstance(value, dict):
        value = _field(value, "score")
        value = None if value is _MISSING else value
    if value is None:
        return None
    if type(value) not in (int, float) or not 0 <= value <= 100:
        raise InvalidEvent(f"{name} must be 0..100 or null")
    return round(value)


def _strings(value, name, *, limit, length):
    if value is _MISSING or value is None:
        return []
    if (not isinstance(value, list) or len(value) > limit or
            any(not isinstance(item, str) or len(item) > length for item in value)):
        raise InvalidEvent(f"{name} must be a list of strings")
    return [item.strip() for item in value if item.strip()]


def parse_summary_request(raw):
    if not isinstance(raw, bytes) or len(raw) > 524_288:
        raise InvalidEvent("request must be UTF-8 JSON within 512 KiB")
    try:
        body = json.loads(raw.decode("utf-8"), object_pairs_hook=_unique_pairs,
                          parse_constant=_reject_constant)
    except (UnicodeDecodeError, json.JSONDecodeError, RecursionError) as exc:
        raise InvalidEvent("invalid request JSON") from exc
    if not isinstance(body, dict):
        raise InvalidEvent("request must be an object")
    sourcecraft_uuid = body.get("sourcecraft_uuid")
    _uuid(sourcecraft_uuid)
    visibility = body.get("visibility")
    if visibility not in VISIBILITIES:
        raise InvalidEvent("visibility must be public or private")
    slugs = {name: body.get(name) for name in ("org_slug", "repo_slug")}
    if any(not isinstance(value, str) or not value.strip() or len(value) > 200
           for value in slugs.values()):
        raise InvalidEvent("org_slug and repo_slug are required")
    _timestamp(body.get("requested_at"), "requested_at")
    grade = body.get("grade")
    if not isinstance(grade, dict):
        raise InvalidEvent("grade must be an object")

    score = _field(grade, "score")
    categories = _field(grade, "category_scores")
    if categories is _MISSING or categories is None:
        categories = {}
    if not isinstance(categories, dict) or len(categories) > 20:
        raise InvalidEvent("category_scores must be an object")
    category_scores = {}
    for name, value in categories.items():
        if not name.strip() or len(name) > 64:
            raise InvalidEvent("invalid category name")
        category_scores[name] = _score(value, f"category_scores.{name}")
    return {
        "sourcecraft_uuid": sourcecraft_uuid,
        "visibility": visibility,
        "org_slug": slugs["org_slug"],
        "repo_slug": slugs["repo_slug"],
        "requested_at": body["requested_at"],
        "score": _score(None if score is _MISSING else score, "score"),
        "category_scores": category_scores,
        "advice": _strings(_field(grade, "advice"), "advice", limit=50, length=2000),
        "missing_data": _strings(_field(grade, "missing_data"), "missing_data",
                                 limit=20, length=64),
    }


def request_correlation_if_valid(raw):
    """Recover sourcecraft_uuid and visibility from an invalid request, if trustworthy."""
    try:
        body = json.loads(raw.decode("utf-8"), object_pairs_hook=_unique_pairs,
                          parse_constant=_reject_constant)
        value = body.get("sourcecraft_uuid") if isinstance(body, dict) else None
        _uuid(value)
    except (AttributeError, UnicodeDecodeError, json.JSONDecodeError, InvalidEvent,
            RecursionError):
        return None, ""
    visibility = body.get("visibility")
    return value, visibility if visibility in VISIBILITIES else ""


def _now():
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def build_summary_result(request, raw_response, model_used):
    return {
        "sourcecraft_uuid": request["sourcecraft_uuid"],
        "raw_response": raw_response,
        "model_used": model_used,
        "evaluated_at": _now(),
        "visibility": request["visibility"],
        "error": "",
    }


def build_summary_failure(sourcecraft_uuid, visibility, error_code, model_used):
    _uuid(sourcecraft_uuid)
    if not isinstance(error_code, str) or not re.fullmatch(r"[A-Z][A-Z0-9_]*", error_code):
        raise ValueError("error code must be a nonempty uppercase identifier")
    return {
        "sourcecraft_uuid": sourcecraft_uuid,
        "raw_response": None,
        "model_used": model_used,
        "evaluated_at": _now(),
        "visibility": visibility,
        "error": error_code,
    }
