"""Local repository grading command; JSON goes to stdout, progress to stderr."""

import argparse
import hashlib
import json
import subprocess
import sys
from pathlib import Path

from .context import build_model_context, select_context_paths
from .discovery import readme_links
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
    pending = sorted(select_context_paths(frozenset(paths)))
    initial_count = len(pending)
    for index, name in enumerate(pending):
        with (root / name).open("rb") as handle:
            data = handle.read(2_000_001)
        total += len(data)
        if len(data) > 2_000_000 or total > 8_000_000:
            raise ValueError("Selected candidate files exceed the local scan limit")
        files[name] = data.decode("utf-8")
        hashes[name] = hashlib.sha256(data).hexdigest()
        if index + 1 == initial_count:
            pending.extend(p for p in readme_links(files, paths) if p not in files)
    return Snapshot(frozenset(paths), files, hashes)


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description="Оценка документации локального Git-репозитория")
    parser.add_argument("--repo", type=Path, default=Path.cwd())
    parser.add_argument("--dry-run", action="store_true", help="Подготовить контекст без вызова модели")
    parser.add_argument("--listen", action="store_true", help="Обрабатывать запросы документации из Kafka")
    parser.add_argument("--listen-summary", action="store_true",
                        help="Обрабатывать запросы summary и рекомендаций из Kafka")
    parser.add_argument("--summary-file", type=Path,
                        help="Сформировать summary по JSON-запросу из файла, без Kafka")
    args = parser.parse_args(argv)
    if sum(map(bool, (args.listen, args.listen_summary, args.summary_file))) > 1:
        parser.error("--listen, --listen-summary and --summary-file are mutually exclusive")
    if args.dry_run and (args.listen or args.listen_summary or args.summary_file):
        parser.error("--dry-run applies only to documentation grading of --repo")
    try:
        if args.listen:
            from .kafka_worker import main as run_kafka_worker
            run_kafka_worker()
            return 0
        if args.listen_summary:
            from .kafka_worker import summary_main
            summary_main()
            return 0
        if args.summary_file:
            from .kafka_worker import process_summary_request
            model = YandexModel(YandexModelConfig.from_env())
            print("Отправляю запрос summary в модель…", file=sys.stderr)
            result = process_summary_request(args.summary_file.read_bytes(), model)
            if result is None:
                print("Ошибка: запрос не содержит корректный sourcecraft_uuid.", file=sys.stderr)
                return 1
            print(json.dumps(result, ensure_ascii=False, indent=2, allow_nan=False))
            return 0 if not result["error"] else 1
        snapshot = local_snapshot(args.repo)
        context = build_model_context(snapshot)
        if args.dry_run:
            print(json.dumps({"selected_files": sorted(snapshot.files),
                              "total_file_count": len(snapshot.all_paths),
                              "context_bytes": len(context.text.encode("utf-8")),
                              "review_mode": "sampled-documentation-v2",
                              "coverage": json.loads(context.text)["coverage"],
                              "sampled_sections": [{k: item[k] for k in
                                  ("path", "heading", "topic", "start_line", "end_line", "excerpt_truncated", "check_ids")}
                                  for item in json.loads(context.text)["documents"]]},
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
