"""Provider-independent boundary for structured Documentation model responses."""

from __future__ import annotations

import json

from .context import (ContextBuildError, ModelContext, allowed_not_applicable_checks,
                      build_model_context)
from .deductions import RULES, _COUNT_FIELDS, _LIST_FIELDS
from .detectors import detect_repository_facts
from .grading import COMPONENTS, grade_documentation
from .snapshot import Snapshot


class InvalidModelResponse(ValueError):
    pass


def _unique_pairs(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise InvalidModelResponse(f"duplicate model field: {key}")
        result[key] = value
    return result


def _derive_file_evidence_lines(evidence: object, files: dict[str, str]) -> None:
    """Derive line locations from an exact quote instead of trusting model line numbers."""
    if not isinstance(evidence, list):
        return
    for item in evidence:
        if not isinstance(item, dict) or item.get("type") != "file_content":
            continue
        path, quote = item.get("path"), item.get("quote")
        if not isinstance(path, str) or path not in files or not isinstance(quote, str) or not quote:
            continue  # The normal evidence validator will reject malformed fields.
        content = "\n".join(files[path].splitlines())
        normalized_quote = quote.replace("\r\n", "\n").replace("\r", "\n")
        offset = content.find(normalized_quote)
        if offset < 0:
            raise InvalidModelResponse(f"file quote is not an exact excerpt: {path}")
        start = content.count("\n", 0, offset) + 1
        item["start_line"] = start
        item["end_line"] = start + normalized_quote.count("\n")


def validate_model_response(raw: bytes, snapshot: Snapshot, context: ModelContext, *,
                            allowed_no_data: frozenset[str] = frozenset(),
                            allowed_not_applicable: frozenset[str] | None = None) -> dict:
    """Accept only the 21 model/hybrid checks, then recompute the category result.

    Applicability and unavailable-context decisions come from ordinary code, not
    a model assertion. Detector facts are injected by this service after parsing;
    the model is never allowed to supply its own ``detected_*`` values.
    """
    if not isinstance(raw, bytes) or len(raw) > 512_000:
        raise InvalidModelResponse("model response is not bytes or is too large")
    try:
        current = build_model_context(snapshot)
    except ContextBuildError as exc:
        raise InvalidModelResponse("repository context cannot be rebuilt") from exc
    if (current.sha256 != context.sha256 or current.reviewed_paths != context.reviewed_paths or
            current.selected_file_count != context.selected_file_count or
            current.total_file_count != context.total_file_count or current.text != context.text):
        raise InvalidModelResponse("repository context changed after model input was prepared")
    try:
        body = json.loads(raw.decode("utf-8"), object_pairs_hook=_unique_pairs)
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise InvalidModelResponse("model response is not UTF-8 JSON") from exc
    if not isinstance(body, dict) or set(body) != {"checks"} or not isinstance(body["checks"], list):
        raise InvalidModelResponse("model response must contain only checks")
    expected = set(COMPONENTS)
    if allowed_not_applicable is None:
        allowed_not_applicable = allowed_not_applicable_checks(snapshot, context)
    if not allowed_no_data <= expected or not allowed_not_applicable <= expected:
        raise ValueError("unknown applicability decision")
    semantic = {}
    for check in body["checks"]:
        if not isinstance(check, dict):
            raise InvalidModelResponse("model check is not an object")
        check_id = check.get("check_id")
        if not isinstance(check_id, str) or check_id not in expected or check_id in semantic:
            raise InvalidModelResponse("unknown or duplicate model check")
        if check.get("outcome") == "EVALUATED":
            components = check.get("components")
            if (not isinstance(components, dict) or
                    any(type(value) is not int for value in components.values())):
                raise InvalidModelResponse(f"invalid integer components: {check_id}")
            if type(check.get("points_awarded")) is not int:
                raise InvalidModelResponse(f"invalid points_awarded: {check_id}")
            # Derive this duplicate total from model-scored components.
            check["points_awarded"] = sum(components.values())
        if check.get("outcome") == "NO_DATA" and check_id not in allowed_no_data:
            raise InvalidModelResponse(f"unjustified NO_DATA: {check_id}")
        if check.get("outcome") == "NOT_APPLICABLE" and check_id not in allowed_not_applicable:
            raise InvalidModelResponse(f"unjustified NOT_APPLICABLE: {check_id}")
        semantic[check_id] = check
    if set(semantic) != expected:
        raise InvalidModelResponse(f"missing model checks: {expected - set(semantic)}")
    detected = detect_repository_facts(snapshot)
    trusted_by_deduction = {}
    for check in semantic.values():
        enriched = []
        deductions = check.get("deductions")
        if not isinstance(deductions, list):
            raise InvalidModelResponse("model deductions must be a list")
        for deduction in deductions:
            if not isinstance(deduction, dict) or not isinstance(deduction.get("facts"), dict):
                raise InvalidModelResponse("invalid model deduction")
            facts = dict(deduction["facts"])
            if any(name.startswith("detected_") for name in facts):
                raise InvalidModelResponse("model supplied a detector fact")
            code = deduction.get("code")
            rule = RULES.get(code) if isinstance(code, str) else None
            trusted_facts = dict(detected)
            if rule is not None:
                for name in rule.required | rule.optional:
                    if not name.startswith("detected_"):
                        continue
                    if name in detected:
                        facts[name] = detected[name]
                    elif name in rule.required and name in _LIST_FIELDS:
                        # The complete repository inventory was scanned; an empty
                        # result means this detector found no matching entries.
                        facts[name] = []
                    elif name in rule.required and name in _COUNT_FIELDS:
                        facts[name] = 0
                    if name in facts and name.startswith("detected_"):
                        trusted_facts[name] = facts[name]
            copy = dict(deduction)
            copy["facts"] = facts
            _derive_file_evidence_lines(copy.get("evidence"), snapshot.files)
            enriched.append(copy)
            if isinstance(copy.get("deduction_id"), str):
                trusted_by_deduction[copy["deduction_id"]] = trusted_facts
        check["deductions"] = enriched
    try:
        result = grade_documentation(
            snapshot.files, semantic, context_sha256=context.sha256,
            reviewed_paths=set(context.reviewed_paths), all_paths=set(snapshot.all_paths),
            trusted_facts_by_deduction=trusted_by_deduction,
        )
    except ValueError as exc:
        raise InvalidModelResponse(str(exc)) from exc
    result["context"] = {
        "sha256": context.sha256,
        "selected_file_count": context.selected_file_count,
        "total_file_count": context.total_file_count,
        "truncated": context.truncated,
    }
    return result
