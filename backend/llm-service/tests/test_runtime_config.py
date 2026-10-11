import os
from pathlib import Path
from unittest import mock

import pytest

from llm_service import runtime_config
from llm_service.runtime_config import (SOURCE_DEFAULT, SOURCE_ENV, SOURCE_FILE, SOURCE_OVERRIDE,
                                        ConfigError, Store, parse)

BASE = """
shared:
  clone_timeout: 10m
  static:
    kafka_brokers: localhost:8053
llm:
  llm_docs_mode: algorithmic
  llm_priority_threshold: 75
  yandex_ai_model:
  static:
    llm_kafka_group_id: docs-v1
gateway:
  llm_priority_threshold: 1
admin_editable:
  - {key: llm.llm_docs_mode, type: enum, values: [auto, llm, algorithmic], group: llm, title: Режим}
  - {key: llm.llm_priority_threshold, type: int, min: "0", max: "100", group: llm, title: Порог}
"""


def write(path: Path, text: str) -> Path:
    path.write_text(text, encoding="utf-8")
    return path


def store_for(tmp_path, text=BASE, env=None) -> tuple[Store, Path]:
    path = write(tmp_path / "app.yaml", text)
    with mock.patch.dict(os.environ, env or {}, clear=True):
        return Store(path), path


def expect(store, key, value, source):
    assert store.get(key, "") == value, key
    assert store.source(key) == source, key


def test_layers_without_env(tmp_path):
    store, _ = store_for(tmp_path)
    expect(store, "LLM_PRIORITY_THRESHOLD", "75", SOURCE_FILE)  # own section, not gateway's
    expect(store, "KAFKA_BROKERS", "localhost:8053", SOURCE_FILE)
    expect(store, "LLM_KAFKA_GROUP_ID", "docs-v1", SOURCE_FILE)
    expect(store, "YANDEX_AI_MODEL", "", SOURCE_DEFAULT)
    assert store.get("YANDEX_AI_MODEL") is None
    assert store.get("NOT_IN_FILE", "fallback") == "fallback"


@pytest.mark.parametrize("env_value", ["40", ""])
def test_env_wins_everywhere(tmp_path, env_value):
    env = {"LLM_PRIORITY_THRESHOLD": env_value, "KAFKA_BROKERS": env_value, "LLM_KAFKA_GROUP_ID": env_value}
    store, path = store_for(tmp_path, env=env)
    for key in env:
        expect(store, key, env_value, SOURCE_ENV)

    with pytest.raises(ValueError):  # admin override over env: rejected, nothing stored
        store.set_override("llm.llm_priority_threshold", "90")
    assert store.overrides() == {}
    store.delete_override("llm.llm_priority_threshold")  # tombstone
    expect(store, "LLM_PRIORITY_THRESHOLD", env_value, SOURCE_ENV)

    write(path, BASE.replace("llm_priority_threshold: 75", "llm_priority_threshold: 76"))
    assert store.reload()
    expect(store, "LLM_PRIORITY_THRESHOLD", env_value, SOURCE_ENV)

    with mock.patch.dict(os.environ, {"LLM_PRIORITY_THRESHOLD": "99"}):  # env changes later
        expect(store, "LLM_PRIORITY_THRESHOLD", env_value, SOURCE_ENV)

    assert store.env_locked("llm.llm_priority_threshold")
    assert {"llm.llm_priority_threshold", "shared.static.kafka_brokers",
            "llm.static.llm_kafka_group_id"} <= set(store.env_locked_paths())


def test_old_alias_in_env_beats_new_name_in_file(tmp_path):
    store, _ = store_for(tmp_path, env={"LLM_STRENGTH_THRESHOLD": "60"})
    assert store.get_first("LLM_PRIORITY_THRESHOLD", "LLM_STRENGTH_THRESHOLD") == "60"
    store2, _ = store_for(tmp_path)
    assert store2.get_first("LLM_PRIORITY_THRESHOLD", "LLM_STRENGTH_THRESHOLD") == "75"


def test_loader_never_writes_os_environ(tmp_path):
    store, _ = store_for(tmp_path)
    with mock.patch.dict(os.environ, {}, clear=True):
        store.get("LLM_DOCS_MODE")
        store.set_override("llm.llm_docs_mode", "auto")
        assert "LLM_DOCS_MODE" not in os.environ


def test_override_over_file_and_reset(tmp_path):
    store, path = store_for(tmp_path)
    calls = []
    store.on_change(lambda: calls.append(1))
    store.set_override("llm.llm_docs_mode", "auto")
    expect(store, "LLM_DOCS_MODE", "auto", SOURCE_OVERRIDE)
    write(path, BASE.replace("llm_docs_mode: algorithmic", "llm_docs_mode: llm"))
    store.reload()
    expect(store, "LLM_DOCS_MODE", "auto", SOURCE_OVERRIDE)
    store.delete_override("llm.llm_docs_mode")
    expect(store, "LLM_DOCS_MODE", "llm", SOURCE_FILE)
    assert len(calls) == 3


def test_override_rejections(tmp_path):
    store, _ = store_for(tmp_path)
    for path, value in (("llm.llm_docs_mode", "magic"), ("llm.llm_priority_threshold", "101"),
                        ("llm.static.llm_kafka_group_id", "x"), ("shared.clone_timeout", "1m")):
        with pytest.raises(ValueError):
            store.set_override(path, value)
    store.set_override("gateway.llm_priority_threshold", "5")  # another service's key: ignored
    assert store.overrides() == {}


def test_invalid_reload_keeps_last_good(tmp_path):
    store, path = store_for(tmp_path)
    for bad in ("llm: [1, 2", "llm:\n  llm_docs_mode: [a]\n",
                BASE.replace("llm_docs_mode: algorithmic", "llm_docs_mode: magic")):
        write(path, bad)
        with pytest.raises(ConfigError):
            store.reload()
        assert store.reload() is False  # the same broken content is reported once
        expect(store, "LLM_DOCS_MODE", "algorithmic", SOURCE_FILE)


def test_static_keys_need_restart(tmp_path):
    store, path = store_for(tmp_path)
    write(path, BASE.replace("llm_kafka_group_id: docs-v1", "llm_kafka_group_id: docs-v2"))
    store.reload()
    expect(store, "LLM_KAFKA_GROUP_ID", "docs-v1", SOURCE_FILE)
    assert store.restart_pending() == ["llm.static.llm_kafka_group_id"]
    write(path, BASE)
    store.reload()
    assert store.restart_pending() == []


@pytest.mark.parametrize("text", [
    "llm:\n  a: 1\n  static:\n    a: 2\n",
    "llm:\n  BadKey: 1\n",
    "llm:\n  deep:\n    x: 1\n",
    "llm:\n  a: 1\nadmin_editable:\n  - {key: llm.b, type: int}\n",
    "llm:\n  static:\n    a: 1\nadmin_editable:\n  - {key: llm.static.a, type: int}\n",
    "llm:\n  a: x\nadmin_editable:\n  - {key: llm.a, type: enum}\n",
])
def test_parse_rejects(text):
    with pytest.raises(ConfigError):
        parse(text.encode())


def test_package_functions_before_init():
    with mock.patch.dict(os.environ, {"SOME_KEY": "live"}):
        assert runtime_config.get("SOME_KEY") == "live"


def test_go_durations():
    assert runtime_config._go_duration("1h30m") == 5400
    with pytest.raises(ValueError):
        runtime_config._go_duration("soon")


def test_repository_config_parses_like_go():
    path = Path(__file__).resolve().parents[3] / "config" / "app.yaml"
    if not path.is_file():
        pytest.skip("no config/app.yaml")
    config = parse(path.read_bytes())
    runtime_config._view(config, (runtime_config.SHARED_SECTION, *runtime_config.SERVICE_SECTIONS))


def test_old_alias_in_env_locks_the_new_key(tmp_path):
    store, _ = store_for(tmp_path, env={"LLM_STRENGTH_THRESHOLD": "60"})
    assert store.env_locked("llm.llm_priority_threshold")
    assert "llm.llm_priority_threshold" in store.env_locked_paths()
    with pytest.raises(ValueError):
        store.set_override("llm.llm_priority_threshold", "90")
    assert store.get_first("LLM_PRIORITY_THRESHOLD", "LLM_STRENGTH_THRESHOLD") == "60"


def test_empty_env_name_keeps_app_yaml_out(tmp_path):
    store, _ = store_for(tmp_path, env={"LLM_PRIORITY_THRESHOLD": ""})
    assert store.get_first("LLM_PRIORITY_THRESHOLD", "LLM_STRENGTH_THRESHOLD", default="d") == "d"
