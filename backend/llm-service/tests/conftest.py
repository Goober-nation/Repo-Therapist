import pytest

from llm_service import runtime_config


@pytest.fixture(autouse=True)
def isolated_runtime_config(tmp_path, monkeypatch):
    """Keep tests off the repository's config/app.yaml and drop any store a test installed
    (main() installs one), so each test sees only the environment it sets up."""
    empty = tmp_path / "app.yaml"
    empty.write_text("")
    monkeypatch.setenv("APP_CONFIG", str(empty))
    yield
    runtime_config._store = None
