"""Shared wire validation helpers and the compact Documentation payload format."""

from __future__ import annotations

from uuid import UUID

from .grading import GROUPS


class InvalidEvent(ValueError):
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
