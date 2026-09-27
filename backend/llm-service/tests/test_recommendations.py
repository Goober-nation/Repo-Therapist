import unittest
from copy import deepcopy

from llm_service.deductions import RULES
from llm_service.grade_wire import build_grade_result, parse_grade_request
from llm_service.recommendations import RECOMMENDATIONS, render_advice
from test_kafka_worker import FakeModel, request


class RecommendationTests(unittest.TestCase):
    def test_catalog_covers_all_known_deficiencies(self):
        self.assertEqual(set(RECOMMENDATIONS), set(RULES))

    def test_specific_reason_and_template_without_internal_json(self):
        fact = {'subcategory': 'local_run', 'code': 'DOC_LOCAL_RUN_VERIFICATION_INCOMPLETE',
                'facts': {'description': 'В примере запуска не показан ожидаемый ответ'},
                'evidence': [{'type': 'file_content', 'path': 'README.md', 'start_line': 4,
                              'end_line': 8, 'quote': 'internal quote'},
                             {'type': 'document_review', 'paths': ['README.md'],
                              'context_sha256': 'internal-hash'}]}
        text = render_advice(fact)
        self.assertNotIn('не показан ожидаемый ответ', text)
        self.assertIn('Добавьте ожидаемый вывод', text)
        self.assertNotIn('README.md', text)
        self.assertNotIn('internal', text)
        self.assertNotIn('start_line', text)

    def test_unknown_code_uses_section_fallback_and_preserves_payload(self):
        payload = FakeModel().grade(None, None)
        for section in payload['subcategories']:
            if section['id'] == 'usage':
                section['grade'] = 50
        payload['facts'] = [{'id': 'DOC-1', 'subcategory': 'usage', 'code': 'NEW_CODE',
                             'facts': {'description': 'Пример обрывается до получения результата'},
                             'evidence': []}]
        before = deepcopy(payload)
        event = build_grade_result(parse_grade_request(request()), payload)
        self.assertEqual(payload, before)
        self.assertNotIn('Пример обрывается', event['advice'][0])
        self.assertIn('Дополните пример использования', event['advice'][0])
        self.assertNotIn('NEW_CODE', event['advice'][0])

    def test_no_advice_for_unreviewed_or_perfect_sections(self):
        for grade in (None, 100):
            payload = FakeModel().grade(None, None)
            for section in payload['subcategories']:
                if section['id'] == 'purpose':
                    section['grade'] = grade
            payload['facts'] = [{'id': 'DOC-1', 'subcategory': 'purpose',
                                 'code': 'DOC_PURPOSE_UNCLEAR', 'facts': {}, 'evidence': []}]
            event = build_grade_result(parse_grade_request(request()), payload)
            self.assertEqual(event['advice'], [])

    def test_deterministic_missing_file_has_specific_recommendation(self):
        text = render_advice({'subcategory': 'required_files', 'code': 'DOC_LICENSE_MISSING',
                           'facts': {}, 'evidence': []})
        self.assertNotIn('Файл LICENSE не найден', text)
        self.assertIn('выбранные владельцем проекта условия', text)
        self.assertNotIn('По рассмотренным фрагментам', text)
