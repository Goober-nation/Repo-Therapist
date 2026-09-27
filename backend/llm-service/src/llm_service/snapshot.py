"""Read a stable selection from the Repository service's reusable S3 prefix."""

from __future__ import annotations

from dataclasses import dataclass
from hashlib import sha256
from typing import Callable, Mapping, Protocol


class ObjectStore(Protocol):
    def list_objects(self, prefix: str) -> Mapping[str, tuple[int, str]]:
        """Return full key -> (size, version token such as ETag) for the full prefix."""

    def read_object(self, key: str) -> bytes:
        """Read the complete object bytes."""


class SnapshotError(Exception):
    """An S3 prefix could not be read as one stable repository state."""


@dataclass(frozen=True)
class Snapshot:
    all_paths: frozenset[str]
    files: Mapping[str, str]
    hashes: Mapping[str, str]
    # Required files that exist but are not UTF-8: kept for the presence check,
    # never shown to the model.
    unreadable: frozenset[str] = frozenset()


def _decode(data: bytes, truncated: bool) -> str:
    try:
        return data.decode("utf-8")
    except UnicodeDecodeError as exc:
        # A byte cap can split the last multibyte character; drop only that tail.
        if truncated and exc.end == len(data) and exc.start >= len(data) - 3:
            return data[:exc.start].decode("utf-8")
        raise


def load_snapshot(store: ObjectStore, prefix: str,
                  selected_paths: set[str] | Callable[[frozenset[str]], set[str]], *,
                  max_file_bytes: int = 2_000_000, max_total_bytes: int = 8_000_000,
                  expand_selection=None, required: set[str] | Callable | None = None,
                  priority: Callable[[str], object] | None = None) -> Snapshot:
    """Compare full listings and reread selected files to detect concurrent writes.

    This is a best-effort consistency check for the current mutable Repository
    prefix. A truly atomic snapshot needs Repository service support later.

    Oversized files are read up to ``max_file_bytes``. Required files are read
    first and always; other files are skipped once ``max_total_bytes`` is spent or
    when they are not UTF-8. A required non-UTF-8 file is kept as unreadable.
    """
    if not prefix or prefix.startswith("/") or prefix.endswith("/") or ".." in prefix.split("/"):
        raise ValueError("invalid S3 prefix")
    root = prefix + "/"
    try:
        before = dict(store.list_objects(root))
    except Exception as exc:
        raise SnapshotError("S3 listing failed") from exc
    if not before:
        raise SnapshotError("S3 prefix not found")
    if any(not key.startswith(root) for key in before):
        raise SnapshotError("object listing escaped repository prefix")
    repository_keys = {key[len(root):]: key for key in before
                       if key[len(root):] and not key.endswith("/")
                       and not key[len(root):].startswith("_meta/")}
    inventory = frozenset(repository_keys)
    if callable(selected_paths):
        selected_paths = selected_paths(inventory)
    if not isinstance(selected_paths, set):
        raise ValueError("selected_paths must be a set")
    if not selected_paths <= repository_keys.keys():
        raise SnapshotError("selected file absent from complete listing")
    required = set(required(inventory) if callable(required) else required or ())
    order = priority or (lambda path: path)
    files = {}
    hashes = {}
    unreadable = set()
    total = 0

    def read_paths(paths):
        nonlocal total
        for path in sorted(paths, key=lambda item: (item not in required, order(item), item)):
            key = repository_keys[path]
            listed_size = before[key][0]
            expected = min(listed_size, max_file_bytes)
            if path not in required and total + expected > max_total_bytes:
                continue
            try:
                first = store.read_object(key)[:max_file_bytes]
                second = store.read_object(key)[:max_file_bytes]
            except Exception as exc:
                raise SnapshotError("S3 read failed") from exc
            if first != second or len(first) != expected:
                raise SnapshotError("S3 content changed during read")
            try:
                text = _decode(first, listed_size > max_file_bytes)
            except UnicodeDecodeError:
                if path not in required:
                    continue
                unreadable.add(path)
                text = first.decode("utf-8", errors="replace")
            total += len(first)
            files[path] = text
            hashes[path] = sha256(first).hexdigest()

    read_paths(selected_paths)
    if expand_selection is not None:
        readable = {path: text for path, text in files.items() if path not in unreadable}
        extra = set(expand_selection(readable, inventory)) - set(files)
        if not extra <= repository_keys.keys():
            raise SnapshotError("linked file absent from complete listing")
        read_paths(extra)
    try:
        after = dict(store.list_objects(root))
    except Exception as exc:
        raise SnapshotError("S3 listing failed") from exc
    if before != after:
        raise SnapshotError("S3 object listing changed during read")
    return Snapshot(inventory, files, hashes, frozenset(unreadable))
