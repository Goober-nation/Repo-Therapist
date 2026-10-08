"""Prometheus metrics of the LLM service: model calls, their outcome and duration, and the tokens the
provider reports. Counts only: no repository, user or prompt text ever becomes a label.

The numbers come from the provider's own ``usage`` object in each reply, so they match what is billed.
Prices are deliberately not here: the dashboard shows tokens, not money.
"""

from __future__ import annotations

import logging

from prometheus_client import CollectorRegistry, Counter, Histogram, ProcessCollector, start_http_server

LOG = logging.getLogger(__name__)

REGISTRY = CollectorRegistry()
ProcessCollector(registry=REGISTRY)

# Fixed words only: anything else is folded into "other" so a bug cannot create unbounded series.
PURPOSES = ("summary", "documentation")
OUTCOMES = ("ok", "rate_limited", "content_filtered", "timeout", "error")
# cached_input is the part of input the provider served from its cache (a subset of input).
KINDS = ("input", "output", "cached_input")

_CALLS = Counter(
    "llm_model_calls_total",
    "Model calls by model, purpose (summary, documentation) and outcome; a format retry is a second call.",
    ["model", "purpose", "outcome"],
    registry=REGISTRY,
)
_TOKENS = Counter(
    "llm_model_tokens_total",
    "Tokens the provider reported for model calls, by model, purpose and kind (input, output, cached_input).",
    ["model", "purpose", "kind"],
    registry=REGISTRY,
)
_DURATION = Histogram(
    "llm_model_call_duration_seconds",
    "Time of a model call, from sending the request to a parsed or failed reply.",
    ["model", "purpose"],
    buckets=(1, 2, 5, 10, 20, 30, 45, 60, 90, 120, 300),
    registry=REGISTRY,
)


def init_series(models) -> None:
    """Create every known series at zero, so the first call is visible to Prometheus' increase()."""
    for model in models:
        for purpose in PURPOSES:
            _DURATION.labels(model, purpose)
            for outcome in OUTCOMES:
                _CALLS.labels(model, purpose, outcome)
            for kind in KINDS:
                _TOKENS.labels(model, purpose, kind)


def _purpose(purpose: str) -> str:
    return purpose if purpose in PURPOSES else "other"


def record_call(model: str, purpose: str, outcome: str, seconds: float) -> None:
    outcome = outcome if outcome in OUTCOMES else "error"
    _CALLS.labels(model, _purpose(purpose), outcome).inc()
    _DURATION.labels(model, _purpose(purpose)).observe(max(0.0, seconds))


def _count(value) -> int | None:
    """A token count from the provider: a non-negative integer, otherwise unusable (None)."""
    if isinstance(value, bool) or not isinstance(value, int) or value < 0:
        return None
    return value


def record_usage(model: str, purpose: str, usage) -> None:
    """Count the tokens of one reply from its ``usage`` object; a missing or malformed one counts nothing."""
    if not isinstance(usage, dict):
        return
    purpose = _purpose(purpose)
    input_tokens = _count(usage.get("input_tokens"))
    output_tokens = _count(usage.get("output_tokens"))
    if input_tokens is not None:
        _TOKENS.labels(model, purpose, "input").inc(input_tokens)
        details = usage.get("input_tokens_details")
        cached = _count(details.get("cached_tokens")) if isinstance(details, dict) else None
        if cached is not None and cached <= input_tokens:
            _TOKENS.labels(model, purpose, "cached_input").inc(cached)
    if output_tokens is not None:
        _TOKENS.labels(model, purpose, "output").inc(output_tokens)


def start_server(port: int | None) -> None:
    """Serve /metrics on ``port`` for Prometheus; None leaves it off. The port is internal, never published."""
    if port is None:
        LOG.info("metrics endpoint is off (LLM_METRICS_PORT is not set)")
        return
    start_http_server(port, registry=REGISTRY)
    LOG.info("metrics endpoint listening on :%d", port)


def port_from_env(environ) -> int | None:
    """LLM_METRICS_PORT as a port number; empty or invalid means off."""
    raw = (environ.get("LLM_METRICS_PORT") or "").strip()
    if not raw.isdigit() or not 1 <= int(raw) <= 65535:
        return None
    return int(raw)
