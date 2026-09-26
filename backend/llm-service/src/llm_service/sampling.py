"""Deterministic selection of short documentation excerpts for the hackathon budget."""
import re

TOPICS = {
    "purpose": r"overview|about|introduction|features|summary|назначени|описани|возможност|о проекте",
    "local_run": r"install|setup|getting.started|quick.start|run|deploy|config|requirements|установ|запуск|настрой|развёр|развер|требован",
    "build": r"build|compil|сборк|компиля",
    "testing": r"test|тест|провер",
    "structure": r"architect|structure|layout|navigation|структур|архитект|навигац",
    "usage": r"usage|example|api|использован|пример",
}


def topic(text):
    return next((name for name, pattern in TOPICS.items()
                 if re.search(pattern, text, re.I)), None)


def file_priority(path):
    lower = path.lower()
    name = lower.rsplit("/", 1)[-1]
    return (0 if lower.startswith("readme") else
            1 if name.startswith("readme") else
            2 if topic(name) else 3, lower.count("/"), lower)


def sections(path, text, hint=""):
    lines = text.splitlines()
    headings = []
    fence = None
    for index, line in enumerate(lines):
        marker = re.match(r"^\s{0,3}(`{3,}|~{3,})", line)
        if marker:
            char = marker[1][0]
            if fence is None:
                fence = (char, len(marker[1]))
            elif char == fence[0] and len(marker[1]) >= fence[1]:
                fence = None
            continue
        if fence:
            continue
        match = re.match(r"^\s{0,3}(#{1,6})\s+(.+?)\s*#*\s*$", line)
        if match:
            headings.append((index, match[2], len(match[1])))
    starts = headings if headings and headings[0][0] == 0 else [(0, path.rsplit("/", 1)[-1], 0)] + headings
    if not lines:
        return []
    result = []
    for pos, (start, title, level) in enumerate(starts):
        end = next((offset for offset, _, depth in starts[pos + 1:]
                    if depth <= level or level == 0), len(lines))
        if pos + 1 < len(starts):
            next_start = starts[pos + 1][0]
            if any(line.strip() for line in lines[start + 1:next_start]):
                end = next_start
        kind = topic(title) or topic(hint) or topic(path)
        if start == 0 and path.lower().rsplit("/", 1)[-1].startswith("readme"):
            kind = kind or "purpose"
        # A document without headings is still a useful bounded sample.
        if kind is None and not headings:
            kind = "purpose"
        if kind is None:
            continue
        stop = min(end, start + 30)
        content = "\n".join(lines[start:stop])
        # Line count alone does not bound minified or unusually long text.
        content = content.encode("utf-8")[:3000].decode("utf-8", errors="ignore")
        result.append({"path": path, "heading": title[:200], "topic": kind,
                       "start_line": start + 1, "end_line": start + len(content.splitlines()),
                       "text": content, "discovery_hint": hint, "excerpt_truncated": stop < end or len(content) < len("\n".join(lines[start:stop]))})
    return result


# Signals select evidence; they do not assert that missing signals mean missing docs.
CHECK_HINTS = {
    "purpose.summary": r"overview|about|introduction|назначени|описани|о проекте",
    "purpose.capabilities": r"feature|capabilit|возможност|функци",
    "local_run.prerequisites": r"prerequisit|requirement|version|требован|верси",
    "local_run.dependencies": r"install|dependenc|установ|зависим",
    "local_run.configuration": r"config|environment|env|настрой|переменн",
    "local_run.execution": r"run|start|deploy|запуск|развёр|развер",
    "local_run.verification": r"health|verify|smoke|localhost|провер|успеш",
    "build.commands": r"build|compil|make|сборк|компиля",
    "build.configuration": r"build.*(config|flag|target)|сборк.*(настрой|параметр)|build_type",
    "build.output": r"artifact|binary|output|артефакт|результат сборки|бинар",
    "testing.commands": r"test|pytest|тест",
    "testing.environment": r"fixture|test.*(config|environment|database)|тест.*(сред|настрой|баз)",
    "testing.scope": r"unit|integration|e2e|coverage|покрыти|интеграцион|модульн",
    "usage.primary_example": r"usage|example|использован|пример",
    "usage.inputs": r"argument|parameter|input|параметр|аргумент|входн",
    "usage.expected_result": r"response|output|result|результат|ответ",
    "structure.overview": r"architect|structure|layout|структур|архитект",
    "structure.navigation": r"navigation|contents|оглавлен|навигац|\[[^]]+\]\(",
}


def excerpt_checks(item):
    text = item["heading"] + "\n" + item.get("discovery_hint", "") + "\n" + item["text"]
    checks = {name for name, pattern in CHECK_HINTS.items() if re.search(pattern, text, re.I)}
    if item["topic"] == "purpose":
        checks.add("purpose.summary")
    if checks & {"purpose.summary", "purpose.capabilities", "structure.overview"}:
        checks.add("consistency.description")
    if re.search(r"\[[^]]+\]\(", item["text"]):
        checks.add("consistency.references")
    if re.search(r"```|~~~|\$ |pip |npm |make |go test|python", item["text"]):
        checks.add("consistency.commands")
    return checks


def sample_documents(documents, hints=None):
    from .grading import CHECKS
    candidates = []
    for path in sorted(documents, key=file_priority):
        for item in sections(path, documents[path], (hints or {}).get(path, "")):
            item["check_ids"] = sorted(excerpt_checks(item))
            candidates.append(item)
    selected, covered, groups = [], set(), set()
    budget = 24_000
    while candidates and len(selected) < 8:
        def rank(item):
            fresh = set(item["check_ids"]) - covered
            return (not (not selected and item["path"].lower().rsplit("/", 1)[-1].startswith("readme") and item["start_line"] == 1),
                    -sum(CHECKS[c].maximum for c in fresh), item["topic"] in groups,
                    file_priority(item["path"]), item["start_line"])
        candidates.sort(key=rank)
        item = candidates.pop(0)
        if not (set(item["check_ids"]) - covered):
            continue
        if any(item["path"] == old["path"] and
               item["start_line"] <= old["end_line"] and
               old["start_line"] <= item["end_line"] for old in selected):
            continue
        size = len(item["text"].encode("utf-8"))
        if size > budget:
            continue
        budget -= size
        selected.append(item)
        covered.update(item["check_ids"])
        groups.add(item["topic"])
    return selected
