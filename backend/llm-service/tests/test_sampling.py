import unittest
from llm_service.sampling import sample_documents


class SamplingTests(unittest.TestCase):
    def test_prioritizes_topics_over_long_irrelevant_sections(self):
        text = '# App\nPurpose\n## Changelog\n' + 'history\n' * 1000
        text += '## Installation\nRun app\n## Tests\npytest\n## Build\nmake\n## Architecture\nModules\n'
        samples = sample_documents({'README.md': text})
        self.assertEqual(len(samples), 5)
        self.assertEqual({s['topic'] for s in samples},
                         {'purpose', 'local_run', 'testing', 'build', 'structure'})
        self.assertTrue(all(len(s['text'].splitlines()) <= 30 for s in samples))
        self.assertTrue(all(len(s['text'].encode()) <= 4000 for s in samples))

    def test_code_fences_are_not_headings_and_original_lines_are_preserved(self):
        samples = sample_documents({'README.md': '# App\n```sh\n## Installation\n```\n## Тестирование\npytest\n'})
        tests = next(s for s in samples if s['topic'] == 'testing')
        self.assertEqual(tests['start_line'], 5)
        self.assertNotIn('local_run', {s['topic'] for s in samples})

    def test_parent_section_includes_nested_instructions_and_large_lines_are_bounded(self):
        samples = sample_documents({'docs/install.md': '## Install\n### Steps\n' + 'x' * 10000})
        self.assertIn('### Steps', samples[0]['text'])
        self.assertLessEqual(len(samples[0]['text'].encode()), 4000)
        self.assertTrue(samples[0]['excerpt_truncated'])
