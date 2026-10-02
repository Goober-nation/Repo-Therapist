import os
import unittest
from unittest import mock

from llm_service.kafka_worker import (DEFAULT_REQUEST_MAX_AGE, KafkaConfig, SummaryKafkaConfig,
                                      request_max_age_from_env)
from llm_service.s3_store import S3Config
from llm_service.summary import DEFAULT_PRIORITY_THRESHOLD, priority_threshold_from_env


class TopicEnvTest(unittest.TestCase):
    def test_docs_topics_default_and_override(self):
        with mock.patch.dict(os.environ, {}, clear=True):
            config = KafkaConfig.from_env()
        self.assertEqual((config.request_topic, config.result_topic),
                         ("repo.docs.grade.requested", "repo.docs.graded"))
        env = {"KAFKA_DOCS_GRADE_REQUESTED_TOPIC": "a", "KAFKA_DOCS_GRADED_TOPIC": "b"}
        with mock.patch.dict(os.environ, env, clear=True):
            config = KafkaConfig.from_env()
        self.assertEqual((config.request_topic, config.result_topic), ("a", "b"))

    def test_summary_topics_prefer_shared_names(self):
        env = {"KAFKA_LLM_REQUESTS_TOPIC": "shared", "LLM_SUMMARY_REQUEST_TOPIC": "legacy",
               "LLM_SUMMARY_RESULT_TOPIC": "legacy-result"}
        with mock.patch.dict(os.environ, env, clear=True):
            config = SummaryKafkaConfig.from_env()
        self.assertEqual((config.request_topic, config.result_topic), ("shared", "legacy-result"))


class S3TimeoutEnvTest(unittest.TestCase):
    base = {"S3_ACCESS_KEY": "a", "S3_SECRET_KEY": "s"}

    def test_defaults_and_override(self):
        with mock.patch.dict(os.environ, self.base, clear=True):
            config = S3Config.from_env()
        self.assertEqual((config.connect_timeout, config.read_timeout), (5, 15))
        with mock.patch.dict(os.environ, {**self.base, "S3_READ_TIMEOUT": "40"}, clear=True):
            self.assertEqual(S3Config.from_env().read_timeout, 40)

    def test_invalid_rejected(self):
        for value in ("soon", "0"):
            with mock.patch.dict(os.environ, {**self.base, "S3_CONNECT_TIMEOUT": value}, clear=True):
                with self.assertRaises(ValueError):
                    S3Config.from_env()


class PriorityThresholdEnvTest(unittest.TestCase):
    def test_values(self):
        for value, want in (("", DEFAULT_PRIORITY_THRESHOLD), ("80", 80),
                            ("101", DEFAULT_PRIORITY_THRESHOLD), ("high", DEFAULT_PRIORITY_THRESHOLD)):
            with mock.patch.dict(os.environ, {"LLM_PRIORITY_THRESHOLD": value}, clear=True):
                self.assertEqual(priority_threshold_from_env(), want, value)

    def test_old_name_is_fallback(self):
        with mock.patch.dict(os.environ, {"LLM_STRENGTH_THRESHOLD": "60"}, clear=True):
            self.assertEqual(priority_threshold_from_env(), 60)
        env = {"LLM_STRENGTH_THRESHOLD": "60", "LLM_PRIORITY_THRESHOLD": " "}
        with mock.patch.dict(os.environ, env, clear=True):
            self.assertEqual(priority_threshold_from_env(), 60)

    def test_new_name_wins(self):
        env = {"LLM_STRENGTH_THRESHOLD": "60", "LLM_PRIORITY_THRESHOLD": "90"}
        with mock.patch.dict(os.environ, env, clear=True):
            self.assertEqual(priority_threshold_from_env(), 90)


class RequestMaxAgeEnvTest(unittest.TestCase):
    def test_default_and_override(self):
        with mock.patch.dict(os.environ, {}, clear=True):
            self.assertEqual(request_max_age_from_env(), DEFAULT_REQUEST_MAX_AGE)
        self.assertEqual(DEFAULT_REQUEST_MAX_AGE, 70)
        with mock.patch.dict(os.environ, {"LLM_DOCS_REQUEST_MAX_AGE_SECONDS": "120"}, clear=True):
            self.assertEqual(request_max_age_from_env(), 120)

    def test_invalid_rejected(self):
        for value in ("soon", "0", "-5", "nan"):
            with mock.patch.dict(os.environ, {"LLM_DOCS_REQUEST_MAX_AGE_SECONDS": value}, clear=True):
                with self.assertRaises(ValueError, msg=value):
                    request_max_age_from_env()
