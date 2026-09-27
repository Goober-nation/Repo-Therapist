"""Conservative repository facts derived without executing repository code."""

from __future__ import annotations

import json
import re
import tomllib
from collections import defaultdict

from .snapshot import Snapshot

_IGNORED_DIRECTORIES = {"node_modules", "vendor", ".git", ".venv", "dist", "build",
                        "target", ".next", ".cache"}
_PACKAGE_MANAGERS = {
    "package.json": "npm", "requirements.txt": "pip", "pyproject.toml": "pip",
    "go.mod": "go", "go.work": "go", "cargo.toml": "cargo", "pom.xml": "maven",
    "build.gradle": "gradle", "build.gradle.kts": "gradle",
}
_BUILD_SYSTEMS = {
    "makefile": "make", "dockerfile": "docker", "go.mod": "go", "go.work": "go",
    "cargo.toml": "cargo", "pom.xml": "maven", "build.gradle": "gradle",
    "build.gradle.kts": "gradle", "setup.py": "setuptools",
}
_LANGUAGE_SUFFIXES = {".go": "go", ".rs": "rust", ".java": "java",
                      ".kt": "kotlin", ".c": "c", ".cc": "cpp", ".cpp": "cpp",
                      ".cs": "csharp", ".swift": "swift"}


def _entry(kind: str, identifier: str, path: str) -> dict[str, str]:
    return {"kind": kind, "identifier": identifier, "path": path}


def detect_repository_facts(snapshot: Snapshot) -> dict[str, object]:
    """Return only facts directly supported by listed paths or selected manifests.

    Values are deliberately sparse. An absent detector fact means the service
    could not verify it; the model must not fill that gap with a guess.
    """
    paths = sorted(path for path in snapshot.all_paths if not any(
        part.lower() in _IGNORED_DIRECTORIES for part in path.split("/")[:-1]))
    contents = snapshot.files
    found: dict[str, list[dict[str, str]]] = defaultdict(list)
    test_types = set()
    env_names = set()
    components: dict[str, str] = {}
    compiled_languages = set()

    def add(field: str, kind: str, identifier: str, path: str) -> None:
        if not kind or not identifier or len(kind) > 256 or len(identifier) > 256:
            return
        item = _entry(kind, identifier, path)
        if item not in found[field]:
            found[field].append(item)

    for path in paths:
        lower = path.lower()
        name = lower.rsplit("/", 1)[-1]
        if "/" in path:
            top = path.split("/", 1)[0]
            components.setdefault(top, path)

        if name in _PACKAGE_MANAGERS:
            add("detected_package_managers", _PACKAGE_MANAGERS[name], name, path)
        elif name.startswith("requirements-") and name.endswith(".txt"):
            add("detected_package_managers", "pip", name, path)
        if name in _BUILD_SYSTEMS:
            add("detected_build_systems", _BUILD_SYSTEMS[name], name, path)
        elif name.startswith("dockerfile."):
            add("detected_build_systems", "docker", name, path)
        if name in {"build.sh", "build.ps1"}:
            add("detected_build_systems", "build_script", path, path)
        for suffix, language in _LANGUAGE_SUFFIXES.items():
            if lower.endswith(suffix) and language not in compiled_languages:
                compiled_languages.add(language)
                add("detected_build_systems", "compiled_source", language, path)
                break
        if name == "dockerfile" or name.startswith("dockerfile."):
            add("detected_artifacts", "container_image", "Dockerfile image", path)
        if name in {"setup.py", "pom.xml", "cargo.toml"}:
            add("detected_artifacts", "package", name, path)

        if name in {"main.py", "cli.py", "__main__.py"} or (
            name == "main.go" and lower.startswith("cmd/")
        ):
            add("detected_entrypoints", "cli", path, path)
            add("detected_interfaces", "cli", path, path)
        if name == "index.html" and ("/" not in lower or lower.startswith(("frontend/", "public/"))):
            add("detected_interfaces", "user_interface", path, path)
        if lower.startswith(("api/", "routes/", "src/api/", "src/routes/")):
            add("detected_interfaces", "http_api", path, path)
        if lower.endswith(".tf"):
            add("detected_interfaces", "infrastructure", path, path)

        if name.startswith("test_") and name.endswith(".py") or name.endswith((
            "_test.go", ".test.js", ".test.ts", ".spec.js", ".spec.ts",
            ".test.jsx", ".test.tsx", ".spec.jsx", ".spec.tsx",
        )):
            test_types.add(_test_type(lower))
        elif lower.startswith("tests/") or "/tests/" in lower:
            test_types.add(_test_type(lower))

    for top, path in sorted(components.items()):
        add("detected_components", "directory", top, path)

    for package_path in (path for path in paths if path.lower().endswith("/package.json")
                         or path.lower() == "package.json"):
        if package_path not in contents:
            continue
        try:
            package = json.loads(contents[package_path])
        except json.JSONDecodeError:
            package = None
        if isinstance(package, dict):
            scripts = package.get("scripts")
            if isinstance(scripts, dict):
                for script, field in (("build", "detected_build_systems"),
                                      ("test", "detected_test_commands"),
                                      ("start", "detected_entrypoints")):
                    if isinstance(scripts.get(script), str) and scripts[script].strip():
                        add(field, "npm_script", f"npm run {script}", package_path)
            if isinstance(package.get("bin"), (str, dict)):
                add("detected_interfaces", "cli", "package bin", package_path)

    for pyproject_path in (path for path in paths if path.lower().endswith("/pyproject.toml")
                           or path.lower() == "pyproject.toml"):
        if pyproject_path not in contents:
            continue
        try:
            project = tomllib.loads(contents[pyproject_path])
        except tomllib.TOMLDecodeError:
            project = None
        if isinstance(project, dict):
            if isinstance(project.get("build-system"), dict):
                add("detected_build_systems", "python_build", "[build-system]", pyproject_path)
                add("detected_artifacts", "package", "Python package", pyproject_path)
            python = project.get("project", {}).get("requires-python") if isinstance(
                project.get("project"), dict) else None
            if isinstance(python, str) and python:
                add("detected_requirements", "python_version", python, pyproject_path)

    for makefile_path in (path for path in paths if path.lower().rsplit("/", 1)[-1] == "makefile"):
        if makefile_path in contents and re.search(r"(?m)^test\s*:", contents[makefile_path]):
            add("detected_test_commands", "make_target", "make test", makefile_path)

    for dockerfile_path in (path for path in paths if path.lower().rsplit("/", 1)[-1] == "dockerfile"
                            or path.lower().rsplit("/", 1)[-1].startswith("dockerfile.")):
        if dockerfile_path not in contents:
            continue
        for line in contents[dockerfile_path].splitlines():
            match = re.match(r"\s*FROM\s+([^\s#]+)", line, flags=re.IGNORECASE)
            if match:
                add("detected_requirements", "container_base", match.group(1), dockerfile_path)
            if re.match(r"\s*(CMD|ENTRYPOINT)\s+", line, flags=re.IGNORECASE):
                add("detected_entrypoints", "container", "Dockerfile CMD/ENTRYPOINT", dockerfile_path)

    for env_path in (path for path in paths if path.lower().rsplit("/", 1)[-1] == ".env.example"):
        if env_path not in contents:
            continue
        for line in contents[env_path].splitlines():
            match = re.match(r"\s*([A-Za-z_][A-Za-z0-9_]*)\s*=", line)
            if match:
                env_names.add(match.group(1))
                add("detected_configuration", "environment_variable", match.group(1), env_path)

    for path in paths:
        if path not in contents or not path.lower().startswith(".github/workflows/"):
            continue
        for line in contents[path].splitlines():
            match = re.match(r"\s*(?:-\s*)?run:\s*(.+?)\s*$", line)
            if not match:
                continue
            command = match.group(1).strip('"\'')
            if re.match(r"^(pytest|go test|npm test|npm run test|cargo test|make test)\b", command):
                add("detected_test_commands", "ci_command", command, path)

    if found["detected_test_commands"] and not test_types:
        test_types.add("other")
    if test_types and not found["detected_test_commands"]:
        found["detected_test_commands"] = []
    if not found["detected_interfaces"] and any(path.lower().startswith("docs/")
                                                 or "/docs/" in path.lower()
                                                 for path in paths) and not any(
        path.lower().endswith((".py", ".go", ".rs", ".js", ".jsx", ".ts", ".tsx",
                               ".java", ".cpp"))
        for path in paths
    ):
        document_path = next(path for path in paths if path.lower().startswith("docs/")
                             or "/docs/" in path.lower())
        add("detected_interfaces", "documentation", "docs", document_path)

    facts: dict[str, object] = {name: sorted(items, key=lambda item: (
        item["kind"], item["identifier"], item["path"])) for name, items in found.items()
        if items or (name == "detected_test_commands" and test_types)}
    if test_types:
        facts["detected_test_types"] = sorted(test_types)
    if env_names:
        facts["detected_identifiers"] = sorted(env_names)
    if components:
        facts["detected_component_count"] = len(components)
    return facts


def _test_type(path: str) -> str:
    if "integration" in path:
        return "integration"
    if "e2e" in path or "end_to_end" in path:
        return "end_to_end"
    if "contract" in path:
        return "contract"
    if "smoke" in path:
        return "smoke"
    if "performance" in path or "benchmark" in path:
        return "performance"
    return "unit"
