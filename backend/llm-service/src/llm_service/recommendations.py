"""Deterministic improvement recommendations for the Grade service.

The texts and priorities live in config/advice.yaml (LLM_ADVICE_PATH overrides the path)
and are loaded once at import, so a missing or incomplete catalog stops the service at
startup. Every advice item is {code, priority, text}: code is the deduction code, or
DOC_SECTION_<ID> for a section fallback, or DOC_ALGORITHMIC_NOTICE for the notice.
"""

from __future__ import annotations

import os
from pathlib import Path

import yaml

from .deductions import RULES
from .grading import GROUPS

# Docker and the documented commands run from backend/llm-service; the source-tree
# path covers imports from any other working directory.
_DEFAULT_PATHS = (Path("config/advice.yaml"),
                  Path(__file__).resolve().parents[2] / "config" / "advice.yaml")


PRIORITIES = ("critical", "high", "medium", "low")
FALLBACK_PRIORITY = "medium"
NOTICE_PRIORITY = "low"
NOTICE_CODE = "DOC_ALGORITHMIC_NOTICE"


class AdviceConfigError(ValueError):
    pass


def fallback_code(group):
    return f"DOC_SECTION_{group.upper()}"


def _item(code, priority, text):
    return {"code": code, "priority": priority, "text": text}


def _text(value, where):
    if not isinstance(value, str) or not value.strip():
        raise AdviceConfigError(f"{where}: expected nonempty text")
    return value.strip()


def advice_path() -> Path:
    configured = os.environ.get("LLM_ADVICE_PATH", "").strip()
    if configured:
        return Path(configured)
    return next((path for path in _DEFAULT_PATHS if path.is_file()), _DEFAULT_PATHS[0])


def load_advice(path: Path) -> tuple[dict, dict, dict]:
    """Return advice items: (by deduction code, fallbacks by section, algorithmic notice)."""
    try:
        data = yaml.safe_load(path.read_text(encoding="utf-8"))
    except (OSError, yaml.YAMLError) as exc:
        raise AdviceConfigError(f"cannot read advice catalog {path}: {exc}") from exc
    if not isinstance(data, dict) or set(data) != {"recommendations", "fallbacks",
                                                   "algorithmic_notice"}:
        raise AdviceConfigError(
            f"{path}: expected keys recommendations, fallbacks, algorithmic_notice")
    if not isinstance(data["recommendations"], list):
        raise AdviceConfigError(f"{path}: recommendations must be a list")
    recommendations = {}
    for index, entry in enumerate(data["recommendations"]):
        where = f"{path}: recommendations[{index}]"
        if not isinstance(entry, dict) or set(entry) != {"codes", "priority", "text"}:
            raise AdviceConfigError(f"{where}: expected codes, priority and text")
        if entry["priority"] not in PRIORITIES:
            raise AdviceConfigError(f"{where}: priority must be one of {', '.join(PRIORITIES)}")
        codes = entry["codes"]
        if not isinstance(codes, list) or not codes:
            raise AdviceConfigError(f"{where}: codes must be a nonempty list")
        text = _text(entry["text"], where)
        for code in codes:
            if code not in RULES:
                raise AdviceConfigError(f"{where}: unknown deduction code {code!r}")
            if code in recommendations:
                raise AdviceConfigError(f"{where}: duplicate code {code}")
            recommendations[code] = _item(code, entry["priority"], text)
    missing = sorted(set(RULES) - set(recommendations))
    if missing:
        raise AdviceConfigError(f"{path}: no recommendation for {', '.join(missing)}")
    fallbacks = data["fallbacks"]
    if not isinstance(fallbacks, dict) or set(fallbacks) != set(GROUPS):
        raise AdviceConfigError(f"{path}: fallbacks must cover exactly {', '.join(GROUPS)}")
    fallbacks = {group: _item(fallback_code(group), FALLBACK_PRIORITY,
                              _text(fallbacks[group], f"{path}: fallbacks.{group}"))
                 for group in GROUPS}
    notice = _item(NOTICE_CODE, NOTICE_PRIORITY,
                   _text(data["algorithmic_notice"], f"{path}: algorithmic_notice"))
    return recommendations, fallbacks, notice


RECOMMENDATIONS, FALLBACKS, ALGORITHMIC_NOTICE = load_advice(advice_path())


def render_advice(fact):
    """Return the improvement recommendation item ({code, priority, text}) for a fact."""
    return RECOMMENDATIONS.get(fact['code'], FALLBACKS[fact['subcategory']])
