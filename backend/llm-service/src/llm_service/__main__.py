"""Local repository grading command; JSON goes to stdout, progress to stderr."""

import argparse
import hashlib
import json
import subprocess
import sys
from pathlib import Path

from .context import build_model_context, select_context_paths
from .model_response import InvalidModelResponse
from .snapshot import Snapshot
from .yandex_model import ModelProviderError, YandexModel, YandexModelConfig


def local_snapshot(directory: Path) -> Snapshot:
    root = Path(subprocess.check_output(
        ["git", "-C", str(directory), "rev-parse", "--show-toplevel"],
        stderr=subprocess.PIPE, text=True).strip()).resolve()
    listing = subprocess.check_output(
        ["git", "-C", str(root), "ls-files", "-z", "--cached", "--others", "--exclude-standard"],
        stderr=subprocess.PIPE)
    paths = set()
    for entry in listing.split(b"\0"):
        if not entry:
            continue
        name = entry.decode("utf-8")
        path = root / name
        if path.is_symlink() or not path.is_file() or not path.resolve().is_relative_to(root):
            continue
        paths.add(name)
    files, hashes = {}, {}
    total = 0
    for name in sorted(select_context_paths(frozenset(paths))):
        with (root / name).open("rb") as handle:
            data = handle.read(512_001)
        total += len(data)
        if total > 512_000:
            raise ValueError("Selected files exceed the 512000-byte local context limit")
        files[name] = data.decode("utf-8")
        hashes[name] = hashlib.sha256(data).hexdigest()
    return Snapshot(frozenset(paths), files, hashes)


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description="Оценка документации локального Git-репозитория")
    parser.add_argument("--repo", type=Path, default=Path.cwd())
    parser.add_argument("--dry-run", action="store_true", help="Подготовить контекст без вызова модели")
    args = parser.parse_args(argv)
    try:
        snapshot = local_snapshot(args.repo)
        context = build_model_context(snapshot)
        if args.dry_run:
            print(json.dumps({"selected_files": sorted(snapshot.files),
                              "total_file_count": len(snapshot.all_paths),
                              "context_bytes": len(context.text.encode("utf-8"))},
                             ensure_ascii=False, indent=2))
            return 0
        model = YandexModel(YandexModelConfig.from_env())
        print(f"Подготовлено файлов: {len(snapshot.files)}. Отправляю запрос в модель…", file=sys.stderr)
        result = model.grade(snapshot, context)
        print(json.dumps(result, ensure_ascii=False, indent=2, allow_nan=False))
        return 0
    except subprocess.CalledProcessError:
        print("Ошибка: не удалось прочитать Git-репозиторий.", file=sys.stderr)
    except (ModelProviderError, InvalidModelResponse, ValueError) as exc:
        print(f"Ошибка: {exc}", file=sys.stderr)
    except OSError:
        print("Ошибка: не удалось прочитать файлы или выполнить системную команду.", file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
