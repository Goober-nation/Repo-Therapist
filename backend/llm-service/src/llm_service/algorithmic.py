"""Model-free Documentation grading used when the LLM is disabled or unavailable.

Only the presence of documentation sections is checked, never their quality. A
section counts as present when any readable document has a matching heading, a
matching file name, or a matching command in code. Doubtful cases count as present.
A present section earns 80% of its points (quality unknown, user decision
2026-09-29); a missing one earns zero. Required files keep their usual presence scoring.
The result has the same shape as the model path: grade, subcategories, facts.
"""

import re

from .context import ModelContext, _is_document, allowed_not_applicable_checks
from .discovery import readme_links
from .documentation_result import documentation_payload
from .formatted_response import _percent
from .grading import CHECKS, GROUPS, METHOD_VERSION, _empty_reason, deterministic_checks
from .sampling import TOPICS
from .snapshot import Snapshot

MISSING_CODES = {
    "purpose": "DOC_PURPOSE_MISSING",
    "local_run": "DOC_LOCAL_RUN_MISSING",
    "build": "DOC_BUILD_INSTRUCTIONS_MISSING",
    "testing": "DOC_TESTING_INSTRUCTIONS_MISSING",
    "usage": "DOC_USAGE_EXAMPLE_MISSING",
    "structure": "DOC_STRUCTURE_OVERVIEW_MISSING",
}
DESCRIPTIONS = {
    "purpose": "В README не найдено описание проекта",
    "local_run": "Не найдена инструкция локального запуска",
    "build": "Не найдено описание сборки",
    "testing": "Не найдено описание запуска тестов",
    "usage": "Не найден пример использования",
    "structure": "Не найдено описание структуры проекта",
}
# Commands in code blocks are strong evidence even under an unusual heading.
COMMANDS = {
    "local_run": (r"\b(pip3? install|poetry install|uv sync|npm (install|ci|start)|"
                  r"npm run (dev|start)|yarn|pnpm (install|dev|start)|docker[ -]compose up|"
                  r"docker run|go run|cargo run|python3? (-m )?\S+|uvicorn|flask run|"
                  r"make (run|start|up|install))\b"),
    "build": (r"\b(make|go build|npm run build|yarn build|pnpm build|cargo build|"
              r"mvn (package|install|compile)|(\./)?gradlew? build|docker build|cmake)\b"),
    "testing": (r"\b(pytest|go test|npm (run )?test|yarn test|pnpm test|cargo test|mvn test|"
                r"(\./)?gradlew? test|make test|unittest|tox|jest|vitest)\b"),
}
PRESENT_SHARE = 0.8
_TREE = re.compile(r"[├└]──")
_HEADING = re.compile(r"^\s{0,3}#{1,6}\s+(.+?)\s*#*\s*$", re.M)
_FENCE = re.compile(r"(?ms)^\s{0,3}(`{3,}|~{3,})[^\n]*\n(.*?)^\s{0,3}\1\s*$")
_INLINE = re.compile(r"`([^`\n]+)`")


def _documents(snapshot: Snapshot) -> dict[str, str]:
    readable = {path: text for path, text in snapshot.files.items()
                if path not in snapshot.unreadable}
    linked = readme_links(readable, snapshot.all_paths)
    return {path: text for path, text in readable.items()
            if _is_document(path) or path in linked}


def _found(group: str, documents: dict[str, str]) -> bool:
    if group == "purpose":
        return any(path.lower().rsplit("/", 1)[-1].startswith("readme") and
                   _empty_reason(text) is None for path, text in documents.items())
    for path, text in documents.items():
        code = "\n".join([block for _, block in _FENCE.findall(text)] + _INLINE.findall(text))
        prose = _FENCE.sub("", text)
        if group in TOPICS and re.search(TOPICS[group], path.rsplit("/", 1)[-1], re.I):
            return True
        if group in TOPICS and any(re.search(TOPICS[group], heading, re.I)
                                   for heading in _HEADING.findall(prose)):
            return True
        if group in COMMANDS and re.search(COMMANDS[group], code, re.I):
            return True
        if group == "structure" and _TREE.search(text):
            return True
    return False


def algorithmic_grade(snapshot: Snapshot, context: ModelContext) -> dict:
    """Score required files as usual and each section by presence: 80% of points or zero."""
    documents = _documents(snapshot)
    allowed = allowed_not_applicable_checks(snapshot, context)
    checks = deterministic_checks(snapshot.files, set(snapshot.all_paths))
    reviewed = sorted(documents)
    for group in MISSING_CODES:
        present = _found(group, documents)
        members = [check_id for check_id, spec in CHECKS.items() if spec.group == group]
        applicable = [check_id for check_id in members if check_id not in allowed]
        for check_id in members:
            maximum = CHECKS[check_id].maximum
            checks[check_id] = (
                {"check_id": check_id, "outcome": "NOT_APPLICABLE", "points_awarded": None,
                 "max_points": maximum, "deductions": []}
                if check_id not in applicable else
                {"check_id": check_id, "outcome": "EVALUATED",
                 "points_awarded": maximum * PRESENT_SHARE if present else 0,
                 "max_points": maximum,
                 "deductions": []})
        if applicable and not present:
            # One fact per missing section, attached to its first applicable check.
            checks[applicable[0]]["deductions"].append({
                "code": MISSING_CODES[group],
                "facts": {"description": DESCRIPTIONS[group], "reviewed_paths": reviewed},
                "evidence": ([{"type": "document_review", "paths": reviewed,
                               "context_sha256": context.sha256}] if reviewed else []),
            })
    for check_id, spec in CHECKS.items():
        # Consistency needs a semantic comparison, so it is never scored here.
        checks.setdefault(check_id, {"check_id": check_id, "outcome": "NOT_REVIEWED",
                                     "points_awarded": None, "max_points": spec.maximum,
                                     "deductions": []})
    ordered = [checks[check_id] for check_id in CHECKS]
    evaluated = [check for check in ordered if check["outcome"] == "EVALUATED"]
    subcategories = []
    for group in GROUPS:
        members = [check for check in evaluated if CHECKS[check["check_id"]].group == group]
        subcategories.append({"id": group, "score": _percent(
            sum(check["points_awarded"] for check in members),
            sum(check["max_points"] for check in members))})
    return documentation_payload({
        "grading_method_version": METHOD_VERSION,
        "score": {"value": _percent(sum(check["points_awarded"] for check in evaluated),
                                    sum(check["max_points"] for check in evaluated))},
        "subcategories": subcategories,
        "checks": ordered,
    })
