"""Grade-service flat Kafka request format."""
import json
import re
from datetime import datetime, timezone
from decimal import Decimal, ROUND_HALF_UP

from .contracts import InvalidEvent, _unique_pairs, _reject_constant, _uuid, validate_documentation_payload
from .recommendations import ALGORITHMIC_NOTICE, FALLBACKS, render_advice

REQUEST_TOPIC = 'repo.docs.grade.requested'
RESULT_TOPIC = 'repo.docs.graded'


def parse_grade_request(raw):
    if not isinstance(raw, bytes) or len(raw) > 524_288:
        raise InvalidEvent('request must be UTF-8 JSON within 512 KiB')
    try:
        request = json.loads(raw.decode('utf-8'), object_pairs_hook=_unique_pairs,
                             parse_constant=_reject_constant)
    except (UnicodeDecodeError, json.JSONDecodeError, RecursionError) as exc:
        raise InvalidEvent('invalid request JSON') from exc
    if not isinstance(request, dict) or set(request) != {'job_id', 'sourcecraft_uuid', 's3_prefix', 'requested_at'}:
        raise InvalidEvent('expected job_id, sourcecraft_uuid, s3_prefix, requested_at')
    _uuid(request['job_id'])
    _uuid(request['sourcecraft_uuid'])
    prefix = request['s3_prefix']
    if (not isinstance(prefix, str) or not prefix or '\\' in prefix or
            any(p in {'', '.', '..'} for p in prefix.split('/'))):
        raise InvalidEvent('invalid repository S3 prefix')
    timestamp = request['requested_at']
    if not isinstance(timestamp, str) or not re.fullmatch(
            r'\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})', timestamp):
        raise InvalidEvent('requested_at must be RFC3339')
    try:
        parsed = datetime.fromisoformat(timestamp.replace('Z', '+00:00'))
        if parsed.tzinfo is None:
            raise ValueError('timezone required')
    except ValueError as exc:
        raise InvalidEvent('requested_at must include a timezone') from exc
    return request


def docs_request_correlation_if_valid(raw):
    """Recover (sourcecraft_uuid, job_id) from an invalid request without trusting other fields.

    The Grade service waits on job_id, so a request without a valid one is uncorrelatable.
    """
    if not isinstance(raw, bytes) or len(raw) > 524_288:
        return None
    try:
        body = json.loads(raw.decode('utf-8'), object_pairs_hook=_unique_pairs,
                          parse_constant=_reject_constant)
        if not isinstance(body, dict):
            return None
        sourcecraft_uuid, job_id = body.get('sourcecraft_uuid'), body.get('job_id')
        _uuid(sourcecraft_uuid)
        _uuid(job_id)
    except (UnicodeDecodeError, json.JSONDecodeError, InvalidEvent, RecursionError):
        return None
    return sourcecraft_uuid, job_id


def _advice(payload, reduced_groups, *, fallbacks=True):
    """One item per recommendation text; a reduced section without facts gets its fallback.

    Algorithmic grades halve every present section, so fallbacks are off there:
    only missing sections and files produce advice.
    """
    advice = []
    covered = set()
    for fact in payload["facts"]:
        if fact["subcategory"] in reduced_groups:
            covered.add(fact["subcategory"])
            advice.append(render_advice(fact))
    if fallbacks:
        advice += [FALLBACKS[item["id"]] for item in payload["subcategories"]
                   if item["id"] in reduced_groups and item["id"] not in covered]
    unique = {}
    for item in advice:
        unique.setdefault(item["text"], dict(item))
    return list(unique.values())


def build_grade_result(request, payload, *, llm_used=True):
    validate_documentation_payload(payload)
    reduced_groups = {item["id"] for item in payload["subcategories"]
                      if item["grade"] is not None and item["grade"] < 100}
    def integer(value):
        return int(Decimal(str(value)).quantize(Decimal("1"), rounding=ROUND_HALF_UP))
    return {
        "job_id": request["job_id"],
        "sourcecraft_uuid": request["sourcecraft_uuid"],
        "grade": integer(payload["grade"]),
        "subgrades": {item["id"]: integer(item["grade"])
                      for item in payload["subcategories"] if item["grade"] is not None},
        "advice": (_advice(payload, reduced_groups, fallbacks=llm_used) +
                   ([] if llm_used else [dict(ALGORITHMIC_NOTICE)])),
        "method": "llm" if llm_used else "algorithmic",
        "error": "",
        "evaluated_at": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
    }


def build_grade_failure(sourcecraft_uuid, job_id, error_code):
    _uuid(sourcecraft_uuid)
    _uuid(job_id)
    if not isinstance(error_code, str) or not error_code or not re.fullmatch(r'[A-Z][A-Z0-9_]*', error_code):
        raise ValueError('error code must be a nonempty uppercase identifier')
    return {
        "job_id": job_id,
        "sourcecraft_uuid": sourcecraft_uuid,
        "grade": None,
        "subgrades": {},
        "advice": [],
        "method": None,
        "error": error_code,
        "evaluated_at": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
    }
