import io
import json
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path
from unittest.mock import patch

from llm_service.__main__ import main
from llm_service.contracts import InvalidEvent
from llm_service.kafka_worker import (SummaryKafkaConfig, process_summary_request,
                                      run_summary_worker)
from llm_service.model_response import InvalidModelResponse
from llm_service.summary import (build_summary_prompt, generate_summary,
                                 parse_summary_response, priority)
from llm_service.summary_wire import parse_summary_request
from llm_service.yandex_model import (ModelDeadlineExceeded, ModelProviderError, YandexModel,
                                      YandexModelConfig)

REPOSITORY = "019b2102-fa21-7ab7-8ce1-116af406fb5e"
SCORES = {"security": 40, "activity": 90, "documentation": 70, "ci_cd": None,
          "issues": 80, "code_health": 55}


def request(visibility="private", **grade):
    body = {"visibility": visibility, "sourcecraft_uuid": REPOSITORY,
            "org_slug": "examples", "repo_slug": "demo",
            "requested_at": "2026-09-27T12:00:00.123456789+03:00",
            "grade": {"SourcecraftUUID": REPOSITORY, "Score": 64,
                      "CategoryScores": dict(SCORES),
                      "Advice": ["Обновите уязвимые зависимости."],
                      "MissingData": ["ci_cd"], "GradedAt": "2026-09-27T11:59:00Z"}}
    body["grade"].update(grade)
    return json.dumps(body, ensure_ascii=False).encode()


def answer(**overrides):
    body = {"summary": "Итоговая оценка 64/100.",
            "recommendations": [
                {"category": "issues", "text": "Разберите старые issues."},
                {"category": "code_health", "text": "Сократите технический долг."},
                {"category": "security", "text": "Обновите зависимости."}]}
    body.update(overrides)
    return json.dumps(body, ensure_ascii=False).encode()


def completed(text):
    return io.BytesIO(json.dumps({"status": "completed", "output": [
        {"type": "message", "content": [{"type": "output_text", "text": text}]}]}).encode())


class FakeModel:
    name = "fake-model"

    def __init__(self, *responses):
        self.responses = list(responses)
        self.prompts = []
        self.schemas = []

    def complete(self, prompt, **kwargs):
        self.prompts.append(prompt)
        self.schemas.append(kwargs["json_schema"])
        response = self.responses.pop(0)
        if isinstance(response, Exception):
            raise response
        return response


class SummaryRequestTests(unittest.TestCase):
    def test_accepts_go_field_names_and_nanosecond_timestamps(self):
        parsed = parse_summary_request(request())
        self.assertEqual(parsed["score"], 64)
        self.assertEqual(parsed["category_scores"], SCORES)
        self.assertEqual(parsed["advice"], ["Обновите уязвимые зависимости."])
        self.assertEqual(parsed["missing_data"], ["ci_cd"])

    def test_accepts_snake_case_nulls_and_score_objects(self):
        body = json.loads(request())
        body["grade"] = {"score": None, "category_scores": {"activity": {"score": 71.6}},
                         "advice": None, "missing_data": None}
        parsed = parse_summary_request(json.dumps(body).encode())
        self.assertIsNone(parsed["score"])
        self.assertEqual(parsed["category_scores"], {"activity": 72})
        self.assertEqual((parsed["advice"], parsed["missing_data"]), ([], []))

    def test_rejects_invalid_fields(self):
        cases = [("visibility", "secret"), ("sourcecraft_uuid", "not-a-uuid"),
                 ("repo_slug", ""), ("requested_at", "yesterday"), ("grade", [])]
        for field, value in cases:
            with self.subTest(field=field):
                body = json.loads(request())
                body[field] = value
                with self.assertRaises(InvalidEvent):
                    parse_summary_request(json.dumps(body).encode())
        for grade in ({"Score": 101}, {"CategoryScores": {"activity": "high"}},
                      {"Advice": [1]}, {"Score": 1, "score": 2}):
            with self.subTest(grade=grade):
                body = json.loads(request())
                body["grade"] = grade
                with self.assertRaises(InvalidEvent):
                    parse_summary_request(json.dumps(body).encode())


class SummaryPromptTests(unittest.TestCase):
    def test_public_prompt_hides_private_categories(self):
        prompt = build_summary_prompt(parse_summary_request(request("public")))
        data = json.loads(prompt.split("INPUT_JSON:\n", 1)[1])
        self.assertEqual({item["id"] for item in data["categories"]},
                         {"activity", "documentation", "issues", "code_health"})
        self.assertEqual(data["missing_data"], [])

    def test_prompt_marks_missing_scores_and_keeps_advice(self):
        prompt = build_summary_prompt(parse_summary_request(request()))
        data = json.loads(prompt.split("INPUT_JSON:\n", 1)[1])
        by_id = {item["id"]: item["score"] for item in data["categories"]}
        self.assertEqual(by_id["ci_cd"], "нет данных")
        self.assertEqual(data["weak_categories"], ["code_health", "documentation", "security"])
        self.assertEqual(data["service_recommendations"], ["Обновите уязвимые зависимости."])
        self.assertEqual(data["repository"], "examples/demo")


class SummaryResponseTests(unittest.TestCase):
    scores = {"security": 40, "issues": 80, "code_health": 55, "documentation": 70,
              "activity": 90}

    def test_code_sets_priorities_and_order(self):
        result = parse_summary_response(answer(), self.scores)
        self.assertEqual([(item["priority"], item["category"])
                          for item in result["recommendations"]],
                         [("HIGH", "security"), ("MEDIUM", "code_health"), ("LOW", "issues")])
        self.assertEqual(priority("security", 70), "HIGH")
        self.assertEqual(priority("activity", 49), "HIGH")
        self.assertEqual(priority("activity", 74), "MEDIUM")
        self.assertEqual(priority("activity", 75), "LOW")

    def test_code_lists_every_scored_category_once(self):
        result = parse_summary_response(answer(), self.scores)
        self.assertEqual(result["strengths"],
                         ["Активность проекта: 90/100", "Issues: 80/100"])
        self.assertEqual(result["weaknesses"],
                         ["Безопасность (Security): 40/100",
                          "Состояние кода и технический долг: 55/100",
                          "Документация: 70/100"])

    def test_strips_stray_edge_punctuation(self):
        raw = answer(summary=". Итог 64/100.  ",
                     recommendations=[{"category": "issues", "text": "— Закройте старые issues;"}])
        result = parse_summary_response(raw, self.scores)
        self.assertEqual(result["summary"], "Итог 64/100.")
        self.assertEqual(result["recommendations"][0]["text"], "Закройте старые issues")

    def test_rejects_format_errors(self):
        bad = [b"not json", answer(summary=""), answer(summary=" . "), answer(extra=1),
               answer(recommendations=[{"category": "issues", "text": "x"}] * 9),
               answer(recommendations=[{"priority": "HIGH", "category": "issues", "text": "x"}]),
               answer(recommendations=[{"category": "ci_cd", "text": "x"}])]
        for raw in bad:
            with self.subTest(raw=raw[:60]):
                with self.assertRaises(InvalidModelResponse):
                    parse_summary_response(raw, self.scores)

    def test_retries_once_with_feedback_then_fails(self):
        model = FakeModel(b"{}", answer())
        result = generate_summary(model, parse_summary_request(request()))
        self.assertEqual(len(model.prompts), 2)
        self.assertIn("VALIDATION_FEEDBACK", model.prompts[1])
        self.assertEqual(result["recommendations"][0]["category"], "security")
        with self.assertRaises(InvalidModelResponse):
            generate_summary(FakeModel(b"{}", b"{}"), parse_summary_request(request()))

    def test_schema_allows_only_scored_visible_categories(self):
        model = FakeModel(answer(recommendations=[]))
        generate_summary(model, parse_summary_request(request("public")))
        enum = model.schemas[0]["properties"]["recommendations"]["items"]["properties"][
            "category"]["enum"]
        self.assertEqual(enum, ["activity", "code_health", "documentation", "issues"])


class SummaryWorkerTests(unittest.TestCase):
    def test_success_result_has_exact_contract_fields(self):
        event = process_summary_request(request(), FakeModel(answer()))
        self.assertEqual(set(event), {"sourcecraft_uuid", "raw_response", "model_used",
                                      "evaluated_at", "visibility", "error"})
        self.assertEqual((event["error"], event["visibility"], event["model_used"]),
                         ("", "private", "fake-model"))
        self.assertEqual(event["raw_response"]["summary"], "Итоговая оценка 64/100.")

    def test_failures_are_error_codes_without_response(self):
        cases = [(FakeModel(ModelDeadlineExceeded("slow")), request(), "LLM_TIMEOUT"),
                 (FakeModel(ModelProviderError("secret")), request(), "LLM_PROVIDER_UNAVAILABLE"),
                 (FakeModel(b"{}", b"{}"), request(), "LLM_INVALID_RESPONSE"),
                 (FakeModel(), request(CategoryScores={"ci_cd": None}), "NO_DATA")]
        for model, raw, code in cases:
            with self.subTest(code=code):
                event = process_summary_request(raw, model)
                self.assertEqual(event["error"], code)
                self.assertIsNone(event["raw_response"])
                self.assertEqual(event["visibility"], "private")
                self.assertNotIn("secret", json.dumps(event))

    def test_invalid_request_is_reported_or_skipped(self):
        body = json.loads(request())
        body["grade"] = None
        event = process_summary_request(json.dumps(body).encode(), FakeModel())
        self.assertEqual((event["error"], event["visibility"]), ("INVALID_EVENT", "private"))
        self.assertIsNone(process_summary_request(b"not json", FakeModel()))

    def test_worker_uses_summary_topics_and_commits_after_ack(self):
        events = []

        class Message:
            def value(self):
                return request()

            def error(self):
                return None

        class Consumer:
            committed = 0

            def subscribe(self, topics):
                self.topics = topics

            def poll(self, timeout):
                if self.committed:
                    raise KeyboardInterrupt()
                return Message()

            def commit(self, **kwargs):
                events.append("commit")
                self.committed += 1
                return []

            def close(self):
                events.append("close")

        class Producer:
            def produce(self, topic, *, key, value, on_delivery):
                self.topic, self.key, self.callback = topic, key, on_delivery
                events.append("produce")

            def flush(self, timeout):
                self.callback(None, None)
                return 0

        consumer, producer = Consumer(), Producer()
        with self.assertRaises(KeyboardInterrupt):
            run_summary_worker(SummaryKafkaConfig(), FakeModel(answer()),
                               consumer=consumer, producer=producer)
        self.assertEqual(consumer.topics, ["llm.recommendations.requested"])
        self.assertEqual(producer.topic, "llm.recommendations.received")
        self.assertEqual(producer.key, REPOSITORY.encode())
        self.assertEqual(events, ["produce", "commit", "close"])

    def test_yandex_request_uses_summary_schema_name(self):
        model = YandexModel(YandexModelConfig("private-key", "folder-123"))
        with patch("llm_service.yandex_model.urlopen",
                   return_value=completed(answer().decode())) as send:
            event = process_summary_request(request(), model)
        self.assertEqual(event["model_used"], "aliceai-llm")
        body = json.loads(send.call_args.args[0].data)
        self.assertEqual(body["text"]["format"]["name"], "repository_summary")
        self.assertNotIn("private-key", body["input"])

    def test_cli_summary_file_prints_result(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "request.json"
            path.write_bytes(request())
            output = io.StringIO()
            with patch.dict("os.environ", {"YANDEX_AI_API_KEY": "k", "YANDEX_FOLDER_ID": "f"}), \
                    patch("llm_service.yandex_model.urlopen",
                          return_value=completed(answer().decode())), \
                    redirect_stdout(output), redirect_stderr(io.StringIO()):
                self.assertEqual(main(["--summary-file", str(path)]), 0)
        self.assertEqual(json.loads(output.getvalue())["error"], "")


if __name__ == "__main__":
    unittest.main()
