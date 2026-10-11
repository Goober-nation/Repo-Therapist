"""Service settings from four layers, highest first (same rules as the Go runtimeconfig package):

1. the process environment, snapshotted once at init (secrets, docker-compose overrides);
2. admin overrides (set from the admin panel, delivered over Kafka);
3. config/app.yaml: the ``shared`` section plus the ``llm`` section;
4. the code default at the call site.

The environment always wins: the snapshot is never written back to ``os.environ``, and reloading
app.yaml or receiving an override only rebuilds that layer, so a key set in the environment (even
to an empty string) keeps its environment value for the process lifetime.

A YAML leaf key maps to the environment variable of the same name in upper case. Keys under a
section's ``static:`` are restart-only: a reload that changes one keeps the running value and
logs that a restart is needed. Before ``init`` (tests, tools) ``get`` reads the live environment,
exactly like ``os.environ.get``.
"""

from __future__ import annotations

import hashlib
import logging
import os
import re
import signal
import threading
from dataclasses import dataclass
from pathlib import Path

import yaml

LOG = logging.getLogger(__name__)

SHARED_SECTION = "shared"
EDITABLE_SECTION = "admin_editable"
SERVICE_SECTIONS = ("llm",)
WATCH_INTERVAL = 2.0
_KEY = re.compile(r"^[a-z][a-z0-9_]*$")
_DURATION = re.compile(r"^(\d+(\.\d+)?(ns|us|µs|ms|s|m|h))+$")
_UNITS = {"ns": 1e-9, "us": 1e-6, "µs": 1e-6, "ms": 1e-3, "s": 1.0, "m": 60.0, "h": 3600.0}

SOURCE_ENV, SOURCE_OVERRIDE, SOURCE_FILE, SOURCE_DEFAULT = "env", "override", "file", "default"

# Old names still accepted from the environment, by the name app.yaml uses. An old name set in the
# environment locks the new key exactly like the new name would.
ALIASES = {
    "LLM_PRIORITY_THRESHOLD": ("LLM_STRENGTH_THRESHOLD",),
    "KAFKA_LLM_REQUESTS_TOPIC": ("LLM_SUMMARY_REQUEST_TOPIC",),
    "KAFKA_LLM_RECOMMENDATIONS_TOPIC": ("LLM_SUMMARY_RESULT_TOPIC",),
}


class ConfigError(ValueError):
    """app.yaml is invalid; a service must not start on a config it misreads."""


@dataclass(frozen=True)
class Setting:
    path: str
    section: str
    env: str
    value: str
    set: bool
    static: bool


@dataclass(frozen=True)
class Editable:
    path: str
    type: str
    min: str = ""
    max: str = ""
    values: tuple[str, ...] = ()
    group: str = ""
    title: str = ""

    def validate(self, value: str) -> None:
        """Raise ValueError when value does not fit the key's type and bounds."""
        if self.type == "bool":
            if value.strip().lower() not in ("1", "t", "true", "0", "f", "false"):
                raise ValueError(f"{value!r} is not true/false")
            return
        if self.type == "string":
            return
        if self.type == "enum":
            if value not in self.values:
                raise ValueError(f"{value!r} is not one of {', '.join(self.values)}")
            return
        number = _number(self.type, value)
        if self.min and number < _number(self.type, self.min):
            raise ValueError(f"{value} is below the minimum {self.min}")
        if self.max and number > _number(self.type, self.max):
            raise ValueError(f"{value} is above the maximum {self.max}")


def _go_duration(value: str) -> float:
    """Seconds in a Go duration string such as 90s, 1h30m."""
    text = value.strip()
    if text == "0":
        return 0.0
    if not _DURATION.match(text):
        raise ValueError(f"{value!r} is not a duration (e.g. 90s, 10m, 2h)")
    return sum(float(n) * _UNITS[u] for n, _, u in re.findall(r"(\d+(\.\d+)?)(ns|us|µs|ms|s|m|h)", text))


def _number(kind: str, value: str) -> float:
    try:
        if kind == "int":
            return float(int(value.strip()))
        if kind == "float":
            return float(value.strip())
    except ValueError:
        raise ValueError(f"{value!r} is not a {'whole ' if kind == 'int' else ''}number") from None
    if kind == "duration":
        return _go_duration(value)
    raise ValueError(f"type {kind} has no numeric value")


def _scalar(value) -> tuple[str, bool]:
    """YAML scalar as the string the environment would hold; (\"\", False) for no value."""
    if value is None:
        return "", False
    if isinstance(value, bool):
        return ("true" if value else "false"), True
    if isinstance(value, (dict, list)):
        raise ConfigError("must be a single value (lists are comma-separated strings)")
    return str(value), True


@dataclass(frozen=True)
class ConfigFile:
    settings: dict[str, Setting]
    editable: dict[str, Editable]
    digest: bytes


def parse(data: bytes) -> ConfigFile:
    """Parse and validate app.yaml content (the same checks as the Go loader)."""
    digest = hashlib.sha256(data).digest()
    try:
        root = yaml.safe_load(data) if data else None
    except yaml.YAMLError as exc:
        raise ConfigError(f"parse app.yaml: {exc}") from None
    settings: dict[str, Setting] = {}
    editable: dict[str, Editable] = {}
    if root is None:
        return ConfigFile(settings, editable, digest)
    if not isinstance(root, dict):
        raise ConfigError("app.yaml: top level must be a mapping of sections")
    entries = []
    for section, body in root.items():
        if section == EDITABLE_SECTION:
            entries = body or []
            continue
        if not isinstance(section, str) or not _KEY.match(section):
            raise ConfigError(f"app.yaml: bad section name {section!r}")
        if body is None:
            continue
        if not isinstance(body, dict):
            raise ConfigError(f"app.yaml: section {section} must be a mapping")
        for key, value in body.items():
            if key == "static":
                if value is None:
                    continue
                if not isinstance(value, dict):
                    raise ConfigError(f"app.yaml: {section}.static must be a mapping")
                for skey, svalue in value.items():
                    _add_leaf(settings, section, skey, svalue, True)
                continue
            _add_leaf(settings, section, key, value, False)
    if not isinstance(entries, list):
        raise ConfigError(f"app.yaml: {EDITABLE_SECTION} must be a list")
    for entry in entries:
        _add_editable(settings, editable, entry)
    return ConfigFile(settings, editable, digest)


def _add_leaf(settings, section, key, value, static):
    path = f"{section}.static.{key}" if static else f"{section}.{key}"
    if not isinstance(key, str) or not _KEY.match(key):
        raise ConfigError(f"app.yaml: bad key name {path!r}")
    try:
        text, is_set = _scalar(value)
    except ConfigError as exc:
        raise ConfigError(f"app.yaml: {path} {exc}") from None
    env = key.upper()
    for other in settings.values():
        if other.section == section and other.env == env:
            raise ConfigError(f"app.yaml: {other.path} and {path} both set {env}")
    settings[path] = Setting(path, section, env, text, is_set, static)


def _add_editable(settings, editable, entry):
    if not isinstance(entry, dict) or "key" not in entry:
        raise ConfigError(f"app.yaml {EDITABLE_SECTION}: every entry needs a key")
    path = entry["key"]
    setting = settings.get(path)
    if setting is None:
        raise ConfigError(f"app.yaml {EDITABLE_SECTION}: {path} is not a setting")
    if setting.static:
        raise ConfigError(f"app.yaml {EDITABLE_SECTION}: {path} is static (restart-only) and cannot be edited at runtime")
    if path in editable:
        raise ConfigError(f"app.yaml {EDITABLE_SECTION}: {path} listed twice")
    kind = entry.get("type")
    values = tuple(str(v) for v in entry.get("values") or ())
    if kind not in ("int", "float", "duration", "bool", "string", "enum"):
        raise ConfigError(f"app.yaml {EDITABLE_SECTION}: {path} has unknown type {kind!r}")
    if kind == "enum" and not values:
        raise ConfigError(f"app.yaml {EDITABLE_SECTION}: {path} is an enum without values")
    item = Editable(path, kind, _scalar(entry.get("min"))[0], _scalar(entry.get("max"))[0], values,
                    str(entry.get("group") or ""), str(entry.get("title") or ""))
    for bound in (item.min, item.max):
        if not bound:
            continue
        if kind not in ("int", "float", "duration"):
            raise ConfigError(f"app.yaml {EDITABLE_SECTION}: {path}: min/max only apply to int, float and duration")
        try:
            _number(kind, bound)
        except ValueError as exc:
            raise ConfigError(f"app.yaml {EDITABLE_SECTION}: {path} bound: {exc}") from None
    if setting.set:
        try:
            item.validate(setting.value)
        except ValueError as exc:
            raise ConfigError(f"app.yaml: {path} default: {exc}") from None
    editable[path] = item


def _view(config: ConfigFile, sections) -> dict[str, Setting]:
    view: dict[str, Setting] = {}
    for section in sections:
        for setting in config.settings.values():
            if setting.section != section:
                continue
            if setting.env in view:
                raise ConfigError(f"app.yaml: {view[setting.env].path} and {setting.path} both set "
                                  f"{setting.env} for the same service")
            view[setting.env] = setting
    return view


def default_path() -> Path:
    """APP_CONFIG, else config/app.yaml in the working directory, else the repository's."""
    configured = os.environ.get("APP_CONFIG", "").strip()
    if configured:
        return Path(configured)
    local = Path("config/app.yaml")
    if local.is_file():
        return local
    return Path(__file__).resolve().parents[4] / "config" / "app.yaml"


class Store:
    """One process's resolved configuration."""

    def __init__(self, path: Path, sections=(SHARED_SECTION, *SERVICE_SECTIONS)):
        self.path = Path(path)
        self.sections = tuple(sections)
        self._env = dict(os.environ)
        self._lock = threading.Lock()
        self._overrides: dict[str, str] = {}
        self._listeners = []
        self._pending: set[str] = set()
        self._bad_digest: bytes | None = None
        self._file = self._read()
        self._view = _view(self._file, self.sections)

    def _read(self) -> ConfigFile:
        try:
            data = self.path.read_bytes()
        except FileNotFoundError:
            LOG.warning("config: %s not found, settings come from the environment and code defaults", self.path)
            data = b""
        return parse(data)

    def resolve(self, key: str) -> tuple[str, str]:
        if key in self._env:
            return self._env[key], SOURCE_ENV
        setting = self._view.get(key)
        if setting is None:
            return "", SOURCE_DEFAULT
        if not setting.static:
            with self._lock:
                if setting.path in self._overrides:
                    return self._overrides[setting.path], SOURCE_OVERRIDE
        if not setting.set:
            return "", SOURCE_DEFAULT
        return setting.value, SOURCE_FILE

    def get(self, key: str, default=None):
        """os.environ.get through the layers; default when no layer sets key."""
        value, source = self.resolve(key)
        return default if source == SOURCE_DEFAULT else value

    def get_first(self, *keys: str, default: str = "") -> str:
        """First nonblank of keys, where new names come before their old aliases. When any of the
        names is in the environment (even empty), only the environment decides: its first nonblank
        value, else default; app.yaml and overrides never fill in for a name the environment holds."""
        if any(key in self._env for key in keys):
            for key in keys:
                value = self._env.get(key, "").strip()
                if value:
                    return value
            return default
        for key in keys:
            value = (self.get(key) or "").strip()
            if value:
                return value
        return default

    def _env_sets(self, env_name: str) -> bool:
        """The environment sets env_name itself or one of its old aliases."""
        return env_name in self._env or any(alias in self._env for alias in ALIASES.get(env_name, ()))

    def source(self, key: str) -> str:
        return self.resolve(key)[1]

    def on_change(self, fn) -> None:
        with self._lock:
            self._listeners.append(fn)

    def _notify(self) -> None:
        with self._lock:
            listeners = list(self._listeners)
        for fn in listeners:
            fn()

    def reload(self) -> bool:
        """Re-read the file; an invalid one keeps the last good config (ConfigError raised once)."""
        try:
            data = self.path.read_bytes()
        except FileNotFoundError:
            data = b""
        digest = hashlib.sha256(data).digest()
        if digest == self._file.digest or digest == self._bad_digest:
            return False
        try:
            config = parse(data)
            view = _view(config, self.sections)
        except ConfigError:
            self._bad_digest = digest
            raise
        old = self._view
        for env, prev in old.items():
            if not prev.static:
                continue
            cur = view.get(env)
            if cur is None or not cur.static or cur.set != prev.set or cur.value != prev.value:
                with self._lock:
                    self._pending.add(prev.path)
                LOG.warning("config: %s changed in %s; it is restart-only, the running value is kept "
                            "until restart", prev.path, self.path)
            else:
                with self._lock:  # back to the startup value: no restart needed any more
                    self._pending.discard(prev.path)
            view[env] = prev
        for env, cur in list(view.items()):
            prev = old.get(env)
            if cur.static and (prev is None or not prev.static):
                if prev is None:
                    del view[env]
                else:
                    view[env] = prev
                LOG.warning("config: %s became restart-only in %s; it applies after restart", cur.path, self.path)
        with self._lock:
            self._file, self._view = config, view
            for path in list(self._overrides):
                if path not in config.editable:
                    del self._overrides[path]
                    LOG.warning("config: override of %s dropped, the key is no longer admin-editable", path)
        LOG.info("config: reloaded %s", self.path)
        self._notify()
        return True

    def owns(self, path: str) -> bool:
        return path.split(".", 1)[0] in self.sections

    def env_locked(self, path: str) -> bool:
        setting = self._file.settings.get(path)
        return setting is not None and self._env_sets(setting.env)

    def env_locked_paths(self) -> list[str]:
        return sorted(s.path for s in self._view.values() if self._env_sets(s.env))

    def set_override(self, path: str, value: str) -> None:
        """Apply an admin override; raises ValueError for a key this process must not take.
        Keys of other services are ignored silently."""
        if not self.owns(path):
            return
        item = self._file.editable.get(path)
        if item is None:
            raise ValueError(f"{path} is not admin-editable")
        item.validate(value)
        if self.env_locked(path):
            raise ValueError(f"{path} is set in the environment, which always wins")
        with self._lock:
            changed = self._overrides.get(path) != value
            self._overrides[path] = value
        if changed:
            LOG.info("config: override applied to %s", path)
            self._notify()

    def delete_override(self, path: str) -> None:
        with self._lock:
            had = self._overrides.pop(path, None) is not None
        if had:
            LOG.info("config: override of %s reset to the file value", path)
            self._notify()

    def overrides(self) -> dict[str, str]:
        with self._lock:
            return dict(self._overrides)

    def restart_pending(self) -> list[str]:
        with self._lock:
            return sorted(self._pending)

    def sources(self) -> dict[str, str]:
        """Every setting of this service with its layer; names only, never values."""
        return {s.path: self.source(env) for env, s in self._view.items()}

    def watch(self, interval: float = WATCH_INTERVAL, stop: threading.Event | None = None) -> threading.Thread:
        """Reload on content change (polled) and on SIGHUP, in a daemon thread."""
        stop = stop or threading.Event()
        wake = threading.Event()
        if threading.current_thread() is threading.main_thread():
            signal.signal(signal.SIGHUP, lambda *_: wake.set())

        def loop():
            while not stop.is_set():
                if wake.wait(interval):
                    wake.clear()
                    LOG.info("config: SIGHUP, reloading %s", self.path)
                try:
                    self.reload()
                except ConfigError as exc:
                    LOG.error("config: reload of %s failed, keeping the last good config: %s", self.path, exc)

        thread = threading.Thread(target=loop, name="config-watch", daemon=True)
        thread.start()
        return thread


_store: Store | None = None


def init(path: Path | None = None, watch: bool = True) -> Store:
    """Load app.yaml for this process, install it for get(), and (by default) watch it."""
    global _store
    store = Store(path or default_path())
    _store = store
    sources = store.sources()
    LOG.info("config: %s loaded, sources: %s", store.path,
             " ".join(f"{p}={sources[p]}" for p in sorted(sources)))
    if watch:
        store.watch()
    return store


def default_store() -> Store | None:
    return _store


def get(key: str, default=None):
    """os.environ.get through the installed store; before init it is os.environ.get."""
    if _store is not None:
        return _store.get(key, default)
    return os.environ.get(key, default)


def get_first(*keys: str, default: str = "") -> str:
    """First nonblank of keys (new name first, then old aliases); environment beats everything."""
    if _store is not None:
        return _store.get_first(*keys, default=default)
    for key in keys:
        value = os.environ.get(key, "").strip()
        if value:
            return value
    return default


def on_change(fn) -> None:
    if _store is not None:
        _store.on_change(fn)
