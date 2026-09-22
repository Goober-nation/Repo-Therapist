"""Choose repository evidence and construct one complete, hashable model context."""

from __future__ import annotations

import json
import re
from dataclasses import dataclass
from hashlib import sha256

from .grading import required_file_paths
from .snapshot import ObjectStore, Snapshot, load_snapshot

DOC_EXTENSIONS = (".md", ".rst", ".adoc", ".txt")
ROOT_DOCUMENTS = {"readme", "license", "licence", "contributing", "changelog",
                  "usage", "installation", "quickstart", "architecture"}
ARTIFACT_NAMES = {"pyproject.toml", "requirements.txt", "package.json", "go.mod",
                  "cargo.toml", "pom.xml", "build.gradle", "makefile", "dockerfile",
                  "docker-compose.yml", "docker-compose.yaml", "pytest.ini", "tox.ini",
                  "setup.cfg", "setup.py", ".env.example"}


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
    basename = lower.rsplit("/", 1)[-1]
    stem = basename.split(".", 1)[0]
    if lower.startswith("docs/") and lower.endswith(DOC_EXTENSIONS):
        return True
    if lower.startswith(".github/") and lower.endswith(DOC_EXTENSIONS):
        return True
    return "/" not in lower and stem in ROOT_DOCUMENTS and (
        "." not in basename or lower.endswith(DOC_EXTENSIONS)
    )


def select_context_paths(all_paths: frozenset[str], *, max_paths: int = 10_000) -> set[str]:
    """Select complete docs and small source manifests; never select secrets or source trees."""
    if len(all_paths) > max_paths:
        raise ContextBuildError("repository inventory exceeds context path limit")
    required = required_file_paths(set(all_paths))
    selected = set(required)
    for path in all_paths:
        lower = path.lower()
        if _is_document(path) or lower in ARTIFACT_NAMES or (
            lower.startswith(".github/workflows/") and lower.endswith((".yml", ".yaml"))
        ):
            selected.add(path)
    return selected


def build_model_context(snapshot: Snapshot, *, max_context_bytes: int = 512_000) -> ModelContext:
    """Use only complete selected files; reject overflow instead of silently truncating."""
    selected = set(snapshot.files)
    if not selected <= snapshot.all_paths:
        raise ContextBuildError("selected files are absent from inventory")
    reviewed = frozenset(path for path in selected if _is_document(path))
    data = {
        "repository_paths": sorted(snapshot.all_paths),
        "documents": {path: snapshot.files[path] for path in sorted(reviewed)},
        "artifacts": {path: snapshot.files[path] for path in sorted(selected - reviewed)},
    }
    text = json.dumps(data, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
    encoded = text.encode("utf-8")
    if len(encoded) > max_context_bytes:
        raise ContextBuildError("complete model context exceeds byte limit")
    return ModelContext(text, sha256(encoded).hexdigest(), reviewed, len(selected),
                        len(snapshot.all_paths))


def prepare_documentation_context(store: ObjectStore, prefix: str, *,
                                  max_context_bytes: int = 512_000) -> tuple[Snapshot, ModelContext]:
    """List S3, choose context paths, read them stably, then hash the exact model input."""
    snapshot = load_snapshot(store, prefix, select_context_paths,
                             max_file_bytes=max_context_bytes, max_total_bytes=max_context_bytes)
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
