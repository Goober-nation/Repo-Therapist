"""Find candidate files and construct a bounded, sampled model context."""

from __future__ import annotations

import json
import re
from dataclasses import dataclass
from hashlib import sha256

from .detectors import detect_repository_facts
from .grading import required_file_paths
from .snapshot import ObjectStore, Snapshot, load_snapshot
from .sampling import TOPICS, file_priority, sample_documents, topic
from .discovery import readme_links
from .grading import COMPONENTS

DOC_EXTENSIONS = (".md", ".rst", ".adoc", ".txt")
ROOT_DOCUMENTS = {"readme", "license", "licence", "contributing", "changelog",
                  "usage", "installation", "quickstart", "architecture"}
ARTIFACT_NAMES = {"pyproject.toml", "requirements.txt", "package.json", "go.mod",
                  "cargo.toml", "pom.xml", "build.gradle", "makefile", "dockerfile",
                  "docker-compose.yml", "docker-compose.yaml", "pytest.ini", "tox.ini",
                  "setup.cfg", "setup.py", ".env.example", "go.work",
                  "build.gradle.kts"}
IGNORED_DIRECTORIES = {"node_modules", "vendor", ".git", ".venv", "dist", "build",
                       "target", ".next", ".cache"}


class ContextBuildError(ValueError):
    pass


@dataclass(frozen=True)
class ModelContext:
    text: str
    sha256: str
    reviewed_paths: frozenset[str]
    selected_file_count: int
    total_file_count: int
    truncated: bool = False


def _is_document(path: str) -> bool:
    lower = path.lower()
    if any(part in IGNORED_DIRECTORIES for part in lower.split("/")[:-1]):
        return False
    basename = lower.rsplit("/", 1)[-1]
    stem = basename.split(".", 1)[0]
    if any(part in {"docs", "doc", "documentation"} for part in lower.split("/")[:-1]) and lower.endswith(DOC_EXTENSIONS):
        return True
    if lower.startswith(".github/") and lower.endswith(DOC_EXTENSIONS):
        return True
    return lower.count("/") <= 4 and (stem in ROOT_DOCUMENTS or topic(stem) is not None) and (
        "." not in basename or lower.endswith(DOC_EXTENSIONS)
    )


def select_context_paths(all_paths: frozenset[str]) -> set[str]:
    """Select at most 12 document candidates and eight artifacts plus required files.

    Only path strings are scanned, so large inventories need no path limit.
    """
    required = required_file_paths(set(all_paths))
    selected = set(required)
    for path in all_paths:
        lower = path.lower()
        parts = lower.split("/")
        if any(part in IGNORED_DIRECTORIES for part in parts[:-1]):
            continue
        artifact = parts[-1] in ARTIFACT_NAMES or parts[-1].startswith("requirements-") and (
            parts[-1].endswith(".txt")) or parts[-1].startswith("dockerfile.")
        if _is_document(path) or (artifact and len(parts) <= 5) or (
            lower.startswith(".github/workflows/") and lower.endswith((".yml", ".yaml"))
        ):
            selected.add(path)
    candidates = sorted((p for p in selected if _is_document(p)), key=file_priority)
    documents = candidates[:1]
    for group in TOPICS:
        match = next((p for p in candidates if topic(p.rsplit("/", 1)[-1]) == group), None)
        if match and match not in documents:
            documents.append(match)
    documents += [p for p in candidates if p not in documents][:12 - len(documents)]
    artifacts = sorted((p for p in selected if not _is_document(p)), key=file_priority)[:8]
    return set(documents) | set(artifacts) | required


def build_model_context(snapshot: Snapshot, *, max_context_bytes: int = 32_000) -> ModelContext:
    """Build an explicitly sampled context with bounded excerpts and supporting metadata."""
    # Unreadable (non-UTF-8) required files only support the presence check.
    selected = set(snapshot.files) - snapshot.unreadable
    if not selected <= snapshot.all_paths:
        raise ContextBuildError("selected files are absent from inventory")
    linked = readme_links(snapshot.files, snapshot.all_paths)
    reviewed = frozenset(path for path in selected if _is_document(path) or path in linked)
    document_paths = reviewed
    excerpts = sample_documents({path: snapshot.files[path] for path in reviewed}, linked)
    reviewed = frozenset(item["path"] for item in excerpts)
    paths = sorted(snapshot.all_paths, key=lambda p: (p.count("/"), p))[:100]
    data = {
        "review_mode": "sampled-documentation-v2",
        "coverage": {check: "sampled" if any(check in item["check_ids"] for item in excerpts)
                     else "not_reviewed" for check in COMPONENTS},
        "sampling": {"max_sections": 8, "max_lines_per_section": 30,
                     "total_file_count": len(snapshot.all_paths),
                     "inventory_truncated": len(paths) < len(snapshot.all_paths)},
        "repository_paths": paths,
        "documents": excerpts,
        "artifacts": {path: snapshot.files[path].encode("utf-8")[:1000].decode("utf-8", errors="ignore")
                      for path in sorted(selected) if path not in document_paths},
        "detected_facts": {key: value[:10] if isinstance(value, list) else value
                           for key, value in detect_repository_facts(snapshot).items()},
    }
    def serialize():
        return json.dumps(data, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
    text = serialize()
    # Preserve selected evidence first; auxiliary metadata yields to the hard budget.
    for field in ("artifacts", "detected_facts", "repository_paths"):
        while len(text.encode("utf-8")) > max_context_bytes and data[field]:
            if isinstance(data[field], dict):
                data[field].pop(next(reversed(data[field])))
            else:
                data[field].pop()
                data["sampling"]["inventory_truncated"] = True
            text = serialize()
    # JSON escaping and excerpt metadata also consume the budget. If they exceed
    # it, drop the lowest-priority selected excerpts and recompute coverage.
    while len(text.encode("utf-8")) > max_context_bytes and data["documents"]:
        data["documents"].pop()
        covered = {check for item in data["documents"] for check in item["check_ids"]}
        data["coverage"] = {check: "sampled" if check in covered else "not_reviewed"
                            for check in COMPONENTS}
        text = serialize()
    reviewed = frozenset(item["path"] for item in data["documents"])
    encoded = text.encode("utf-8")
    if len(encoded) > max_context_bytes:
        raise ContextBuildError("complete model context exceeds byte limit")
    return ModelContext(text, sha256(encoded).hexdigest(), reviewed, len(selected),
                        len(snapshot.all_paths), truncated=True)


def prepare_documentation_context(store: ObjectStore, prefix: str, *,
                                  max_context_bytes: int = 32_000) -> tuple[Snapshot, ModelContext]:
    """List S3, choose context paths, read them stably, then hash the exact model input."""
    snapshot = load_snapshot(store, prefix, select_context_paths,
                             max_file_bytes=2_000_000, max_total_bytes=8_000_000,
                             expand_selection=readme_links,
                             required=lambda paths: required_file_paths(set(paths)),
                             priority=file_priority)
    return snapshot, build_model_context(snapshot, max_context_bytes=max_context_bytes)


def allowed_not_applicable_checks(snapshot: Snapshot, context: ModelContext) -> frozenset[str]:
    """Conservatively permit only mechanically supportable N/A outcomes.

    A positive indicator makes a check applicable. Absence of test artifacts
    permits N/A for documentation about tests, not for the test-quality category.
    """
    paths = {path.lower() for path in snapshot.all_paths}
    base_names = {path.rsplit("/", 1)[-1] for path in paths}
    selected = {path.lower(): content for path, content in snapshot.files.items()}
    package = selected.get("package.json")
    package_scripts = {}
    if package is not None:
        try:
            value = json.loads(package)
            if isinstance(value, dict) and isinstance(value.get("scripts"), dict):
                package_scripts = value["scripts"]
        except json.JSONDecodeError:
            # An unreadable manifest cannot justify marking checks N/A.
            package_scripts = {"build": "unknown", "test": "unknown"}

    build_files = {"makefile", "dockerfile", "go.mod", "cargo.toml", "pom.xml",
                   "build.gradle", "build.gradle.kts", "setup.py"}
    compiled_suffixes = (".go", ".rs", ".java", ".kt", ".c", ".cc", ".cpp", ".cs", ".swift")
    has_build = (bool(base_names & build_files) or "build" in package_scripts or
                 any(name.endswith(compiled_suffixes) or name.endswith(("build.sh", "build.ps1"))
                     for name in paths) or bool(re.search(
                         r"(?m)^\s*\[build-system\]\s*$", selected.get("pyproject.toml", ""))))
    test_config = {"pytest.ini", "tox.ini", "jest.config.js", "vitest.config.ts"}
    test_suffixes = ("_test.go", ".test.js", ".test.ts", ".spec.js", ".spec.ts",
                     ".test.jsx", ".test.tsx", ".spec.jsx", ".spec.tsx")
    workflow_text = "\n".join(content for path, content in selected.items()
                              if path.startswith(".github/workflows/"))
    has_tests = ("test" in package_scripts or bool(base_names & test_config) or bool(
        re.search(r"\b(pytest|go test|npm test|vitest|jest|cargo test)\b", workflow_text,
                  flags=re.IGNORECASE)) or any(
        name.startswith("test_") and name.endswith(".py") or
        name.endswith(test_suffixes) or name.startswith("tests/") or
        "/tests/" in name for name in paths
    ))
    detected = detect_repository_facts(snapshot)
    has_build = has_build or bool(detected.get("detected_build_systems"))
    has_tests = has_tests or bool(detected.get("detected_test_types")) or bool(
        detected.get("detected_test_commands"))
    allowed = set()
    if not has_build:
        allowed.update({"build.commands", "build.configuration", "build.output"})
    if not has_tests:
        allowed.update({"testing.commands", "testing.environment", "testing.scope"})
    if not context.reviewed_paths:
        allowed.update({"consistency.references", "consistency.commands",
                        "consistency.description"})
    if len(context.reviewed_paths) <= 1 and sum(len(snapshot.files[path]) for path in
                                                context.reviewed_paths) <= 2_000:
        allowed.add("structure.navigation")
    directories = {path.split("/", 1)[0] for path in paths if "/" in path}
    if len(paths) <= 10 and len(directories) <= 1:
        allowed.add("structure.overview")
    return frozenset(allowed)
