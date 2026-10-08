import io
import json
import unittest
from unittest.mock import patch
from urllib.error import HTTPError, URLError

from prometheus_client import generate_latest

from llm_service import metrics
from llm_service.yandex_model import (ModelContentFiltered, ModelDeadlineExceeded, ModelProviderError,
                                      ModelRateLimited, YandexModel, YandexModelConfig)
from test_summary import answer, completed, request
from llm_service.kafka_worker import process_summary_request

MODEL = "aliceai-llm"


def value(name, **labels):
    """Current value of a series, 0 when it does not exist yet."""
    return metrics.REGISTRY.get_sample_value(name, labels) or 0


def reply(text="ok", usage=None, status="completed", extra=None):
    body = {"status": status,
            "output": [{"type": "message", "content": [{"type": "output_text", "text": text}]}]}
    if usage is not None:
        body["usage"] = usage
    body.update(extra or {})
    return io.BytesIO(json.dumps(body).encode())


class Delta:
    """What a block of code added to the counters, so tests do not depend on earlier tests."""

    def __init__(self, name, **labels):
        self.name, self.labels = name, labels

    def __enter__(self):
        self.before = value(self.name, **self.labels)
        return self

    def __exit__(self, *exc):
        self.added = value(self.name, **self.labels) - self.before
        return False


class ModelCallMetricsTests(unittest.TestCase):
    def setUp(self):
        self.model = YandexModel(YandexModelConfig("private-key", "folder-123"))

    def test_a_good_call_counts_the_call_its_time_and_the_reported_tokens(self):
        usage = {"input_tokens": 16, "input_tokens_details": {"cached_tokens": 4},
                 "output_tokens": 3, "total_tokens": 19}
        with Delta("llm_model_calls_total", model=MODEL, purpose="documentation", outcome="ok") as calls, \
                Delta("llm_model_call_duration_seconds_count", model=MODEL, purpose="documentation") as timed, \
                Delta("llm_model_tokens_total", model=MODEL, purpose="documentation", kind="input") as tokens_in, \
                Delta("llm_model_tokens_total", model=MODEL, purpose="documentation", kind="output") as tokens_out, \
                Delta("llm_model_tokens_total", model=MODEL, purpose="documentation", kind="cached_input") as cached:
            with patch("llm_service.yandex_model.urlopen", return_value=reply("готово", usage)):
                self.model.complete("Ответь: готово")
        self.assertEqual((calls.added, timed.added), (1, 1))
        self.assertEqual((tokens_in.added, tokens_out.added, cached.added), (16, 3, 4))

    def test_the_summary_path_is_labelled_summary(self):
        usage = {"input_tokens": 100, "output_tokens": 40}
        body = json.loads(completed(answer().decode()).read())
        body["usage"] = usage
        with Delta("llm_model_calls_total", model=MODEL, purpose="summary", outcome="ok") as calls, \
                Delta("llm_model_tokens_total", model=MODEL, purpose="summary", kind="output") as out:
            with patch("llm_service.yandex_model.urlopen", return_value=io.BytesIO(json.dumps(body).encode())):
                process_summary_request(request(), self.model)
        self.assertEqual((calls.added, out.added), (1, 40))

    def test_the_label_is_the_configured_model_never_the_provider_uri_with_the_folder(self):
        usage = {"input_tokens": 1, "output_tokens": 1}
        with patch("llm_service.yandex_model.urlopen",
                   return_value=reply("x", usage, extra={"model": "gpt://folder-123/aliceai-llm"})):
            self.model.complete("hi")
        exposed = generate_latest(metrics.REGISTRY).decode()
        self.assertNotIn("folder-123", exposed)
        self.assertNotIn("private-key", exposed)
        self.assertNotIn("gpt://", exposed)

    def test_failures_are_counted_by_outcome_and_add_no_tokens(self):
        cases = {
            "rate_limited": HTTPError("u", 429, "x", {}, None),
            "timeout": URLError(TimeoutError()),
            "error": HTTPError("u", 500, "x", {}, None),
        }
        expected = {"rate_limited": ModelRateLimited, "timeout": ModelDeadlineExceeded, "error": ModelProviderError}
        for outcome, failure in cases.items():
            with self.subTest(outcome=outcome), \
                    Delta("llm_model_calls_total", model=MODEL, purpose="documentation", outcome=outcome) as calls, \
                    Delta("llm_model_tokens_total", model=MODEL, purpose="documentation", kind="input") as tokens:
                with patch("llm_service.yandex_model.urlopen", side_effect=failure):
                    with self.assertRaises(expected[outcome]):
                        self.model.complete("hi")
            self.assertEqual((calls.added, tokens.added), (1, 0))

    def test_a_filtered_reply_still_counts_the_tokens_that_were_billed(self):
        body = reply("", {"input_tokens": 50, "output_tokens": 0}, status="incomplete",
                     extra={"incomplete_details": {"reason": "content_filter"}})
        with Delta("llm_model_calls_total", model=MODEL, purpose="documentation", outcome="content_filtered") as calls, \
                Delta("llm_model_tokens_total", model=MODEL, purpose="documentation", kind="input") as tokens:
            with patch("llm_service.yandex_model.urlopen", return_value=body):
                with self.assertRaises(ModelContentFiltered):
                    self.model.complete("hi")
        self.assertEqual((calls.added, tokens.added), (1, 50))

    def test_a_reply_that_fails_validation_after_the_call_is_still_a_counted_call(self):
        with Delta("llm_model_calls_total", model=MODEL, purpose="documentation", outcome="error") as calls, \
                Delta("llm_model_tokens_total", model=MODEL, purpose="documentation", kind="output") as out:
            with patch("llm_service.yandex_model.urlopen",
                       return_value=reply("", {"input_tokens": 9, "output_tokens": 7}, status="failed")):
                with self.assertRaises(ModelProviderError):
                    self.model.complete("hi")
        self.assertEqual((calls.added, out.added), (1, 7))

    def test_input_that_is_too_large_is_never_sent_and_so_not_counted(self):
        with Delta("llm_model_calls_total", model=MODEL, purpose="documentation", outcome="error") as calls:
            with patch("llm_service.yandex_model.urlopen") as send:
                with self.assertRaises(ModelProviderError):
                    self.model.complete("x" * 300_000)
        send.assert_not_called()
        self.assertEqual(calls.added, 0)


class UsageParsingTests(unittest.TestCase):
    def test_malformed_usage_counts_nothing(self):
        bad = [None, "text", [], {}, {"input_tokens": "5"}, {"input_tokens": -3, "output_tokens": -1},
               {"input_tokens": True, "output_tokens": 2.5}]
        for usage in bad:
            with self.subTest(usage=usage), \
                    Delta("llm_model_tokens_total", model=MODEL, purpose="summary", kind="input") as tokens_in, \
                    Delta("llm_model_tokens_total", model=MODEL, purpose="summary", kind="output") as tokens_out:
                metrics.record_usage(MODEL, "summary", usage)
            self.assertEqual((tokens_in.added, tokens_out.added), (0, 0))

    def test_cached_tokens_are_only_counted_when_they_fit_inside_the_input(self):
        with Delta("llm_model_tokens_total", model=MODEL, purpose="summary", kind="cached_input") as cached:
            metrics.record_usage(MODEL, "summary", {"input_tokens": 10, "output_tokens": 1,
                                                    "input_tokens_details": {"cached_tokens": 99}})
            metrics.record_usage(MODEL, "summary", {"input_tokens": 10, "output_tokens": 1,
                                                    "input_tokens_details": {"cached_tokens": "x"}})
        self.assertEqual(cached.added, 0)

    def test_unknown_purposes_and_outcomes_are_folded_so_labels_stay_bounded(self):
        with Delta("llm_model_calls_total", model=MODEL, purpose="other", outcome="error") as calls:
            metrics.record_call(MODEL, "a-repo-slug", "weird", 0.5)
        self.assertEqual(calls.added, 1)

    def test_known_series_start_at_zero(self):
        for model in ("aliceai-llm", "aliceai-llm-flash"):
            for purpose in metrics.PURPOSES:
                self.assertIsNotNone(metrics.REGISTRY.get_sample_value(
                    "llm_model_calls_total", {"model": model, "purpose": purpose, "outcome": "ok"}))
                self.assertIsNotNone(metrics.REGISTRY.get_sample_value(
                    "llm_model_tokens_total", {"model": model, "purpose": purpose, "kind": "output"}))


class ServerSettingTests(unittest.TestCase):
    def test_the_port_comes_from_the_environment_and_bad_values_turn_it_off(self):
        self.assertEqual(metrics.port_from_env({"LLM_METRICS_PORT": "9104"}), 9104)
        for raw in ("", " ", "abc", "0", "70000", "-1", "9104x"):
            self.assertIsNone(metrics.port_from_env({"LLM_METRICS_PORT": raw}), raw)
        self.assertIsNone(metrics.port_from_env({}))

    def test_no_port_means_no_server(self):
        with patch("llm_service.metrics.start_http_server") as start:
            metrics.start_server(None)
            start.assert_not_called()
            metrics.start_server(9105)
            start.assert_called_once()
            self.assertEqual(start.call_args.args[0], 9105)
