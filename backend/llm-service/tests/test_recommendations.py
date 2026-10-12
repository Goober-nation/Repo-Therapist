import tempfile
import unittest
from copy import deepcopy
from pathlib import Path

from llm_service.deductions import RULES
from llm_service.grade_wire import build_grade_result, parse_grade_request
from llm_service.recommendations import (ALGORITHMIC_NOTICE, FALLBACKS, RECOMMENDATIONS,
                                         AdviceConfigError, advice_path, load_advice,
                                         render_advice)
from test_kafka_worker import FakeModel, request


class RecommendationTests(unittest.TestCase):
    def test_catalog_covers_all_known_deficiencies(self):
        self.assertEqual(set(RECOMMENDATIONS), set(RULES))

    def test_every_item_has_code_priority_and_text(self):
        items = list(RECOMMENDATIONS.values()) + list(FALLBACKS.values()) + [ALGORITHMIC_NOTICE]
        for item in items:
            self.assertEqual(set(item), {'code', 'priority', 'text'})
            self.assertIn(item['priority'], ('critical', 'high', 'medium', 'low'))
        self.assertTrue(all(code == item['code'] for code, item in RECOMMENDATIONS.items()))
        self.assertEqual(ALGORITHMIC_NOTICE['code'], 'DOC_ALGORITHMIC_NOTICE')
        self.assertEqual(ALGORITHMIC_NOTICE['priority'], 'low')

    def test_catalog_is_read_from_yaml_and_validated(self):
        self.assertEqual(advice_path().name, 'advice.yaml')
        good = advice_path().read_text(encoding='utf-8')
        broken = {
            'unknown code': good.replace('codes: [DOC_BUILD_OUTPUT_UNCLEAR]',
                                         'codes: [DOC_BUILD_OUTPUT_UNCLEAR, DOC_NEW]'),
            'missing code': good.replace('codes: [DOC_BUILD_OUTPUT_UNCLEAR]', 'codes: [DOC_X]'),
            'missing section': good.replace('  consistency: ', '  other: '),
            'empty notice': good.replace('algorithmic_notice: "', 'algorithmic_notice: " "#'),
            'unknown priority': good.replace('    priority: high\n', '    priority: urgent\n', 1),
            'no priority': good.replace('    priority: high\n', '', 1),
            'not yaml': 'recommendations: [',
        }
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / 'advice.yaml'
            path.write_text(good, encoding='utf-8')
            self.assertEqual(load_advice(path), (RECOMMENDATIONS, FALLBACKS, ALGORITHMIC_NOTICE))
            for name, text in broken.items():
                with self.subTest(name):
                    path.write_text(text, encoding='utf-8')
                    with self.assertRaises(AdviceConfigError):
                        load_advice(path)
            with self.assertRaises(AdviceConfigError):
                load_advice(Path(tmp) / 'absent.yaml')

    def test_specific_reason_and_template_without_internal_json(self):
        fact = {'subcategory': 'local_run', 'code': 'DOC_LOCAL_RUN_VERIFICATION_INCOMPLETE',
                'facts': {'description': 'В примере запуска не показан ожидаемый ответ'},
                'evidence': [{'type': 'file_content', 'path': 'README.md', 'start_line': 4,
                              'end_line': 8, 'quote': 'internal quote'},
                             {'type': 'document_review', 'paths': ['README.md'],
                              'context_sha256': 'internal-hash'}]}
        item = render_advice(fact)
        self.assertEqual((item['code'], item['priority']),
                         ('DOC_LOCAL_RUN_VERIFICATION_INCOMPLETE', 'low'))
        text = item['text']
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
        self.assertNotIn('Пример обрывается', event['advice'][0]['text'])
        self.assertIn('Дополните пример использования', event['advice'][0]['text'])
        self.assertNotIn('NEW_CODE', event['advice'][0]['text'])
        self.assertEqual((event['advice'][0]['code'], event['advice'][0]['priority']),
                         ('DOC_SECTION_USAGE', 'medium'))

    def test_no_advice_for_unreviewed_or_perfect_sections(self):
        for grade in (None, 100):
            payload = FakeModel().grade(None, None)
            for section in payload['subcategories']:
                section['grade'] = grade if section['id'] == 'purpose' else 100
            payload['facts'] = [{'id': 'DOC-1', 'subcategory': 'purpose',
                                 'code': 'DOC_PURPOSE_UNCLEAR', 'facts': {}, 'evidence': []}]
            event = build_grade_result(parse_grade_request(request()), payload)
            self.assertEqual(event['advice'], [])

    def test_duplicates_removed_and_reduced_section_without_facts_gets_fallback(self):
        payload = FakeModel().grade(None, None)
        for section in payload['subcategories']:
            section['grade'] = {'purpose': 50, 'testing': 80}.get(section['id'], 100)
        fact = {'subcategory': 'purpose', 'code': 'DOC_PURPOSE_UNCLEAR', 'facts': {},
                'evidence': []}
        payload['facts'] = [dict(fact, id=f'DOC-{i}') for i in range(3)]
        event = build_grade_result(parse_grade_request(request()), payload)
        self.assertEqual(event['advice'], [RECOMMENDATIONS['DOC_PURPOSE_UNCLEAR'],
                                           FALLBACKS['testing']])

    def test_deterministic_missing_file_has_specific_recommendation(self):
        item = render_advice({'subcategory': 'required_files', 'code': 'DOC_LICENSE_MISSING',
                              'facts': {}, 'evidence': []})
        self.assertEqual(item['priority'], 'low')
        text = item['text']
        self.assertNotIn('Файл LICENSE не найден', text)
        self.assertIn('выбранные владельцем проекта условия', text)
        self.assertNotIn('По рассмотренным фрагментам', text)
