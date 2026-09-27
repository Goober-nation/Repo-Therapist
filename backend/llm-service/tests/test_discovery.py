import json
import unittest

from llm_service.context import prepare_documentation_context, build_model_context
from llm_service.discovery import readme_links
from llm_service.formatted_response import parse_formatted_response
from test_context import FakeStore
from test_yandex_model import SNAPSHOT, full_checks


class DiscoveryTests(unittest.TestCase):
    def test_internal_inline_reference_and_root_links_only(self):
        files = {'README.md': '[Install](manual/start.md#setup)\n[Tests][t]\n[t]: /manual/tests.md\n[No](https://example.com/x.md)\n[Escape](../x.md)\n[Secret](.env)'}
        paths = {'manual/start.md', 'manual/tests.md', 'x.md', '.env'}
        self.assertEqual(set(readme_links(files, paths)), {'manual/start.md', 'manual/tests.md'})

    def test_s3_follows_readme_link_outside_normal_docs_locations(self):
        store = FakeStore()
        store.objects['repos/id/README.md'] = b'# App\nDescription\n[Installation](manual/start.md)'
        store.objects['repos/id/manual/start.md'] = b'# First steps\npip install app\npython app.py\n'
        snapshot, context = prepare_documentation_context(store, 'repos/id')
        self.assertIn('manual/start.md', snapshot.files)
        data = json.loads(context.text)
        self.assertTrue(any(e['path'] == 'manual/start.md' for e in data['documents']))
        self.assertEqual(data['coverage']['local_run.dependencies'], 'sampled')
        self.assertLessEqual(len(context.text.encode()), 32000)

    def test_unreviewed_checks_do_not_keep_scores_or_model_penalties(self):
        context = build_model_context(SNAPSHOT)
        checks = full_checks()
        coverage = json.loads(context.text)['coverage']
        target = next(c for c in checks if coverage[c['check_id']] == 'not_reviewed')
        target['points_awarded'] = 0
        target['deductions'] = [{'deduction_id': 'invented', 'code': 'UNSEEN',
                                 'raw_points_lost': 1, 'facts': {}, 'evidence': []}]
        result = parse_formatted_response(json.dumps({'checks': checks}).encode(), SNAPSHOT, context)
        actual = next(c for c in result['checks'] if c['check_id'] == target['check_id'])
        self.assertIsNone(actual['points_awarded'])
        self.assertEqual(actual['deductions'], [])
        self.assertEqual(actual['outcome'], 'NOT_REVIEWED')

    def test_unreviewed_groups_are_null_and_excluded_from_denominator(self):
        context = build_model_context(SNAPSHOT)
        result = parse_formatted_response(json.dumps({'checks': full_checks()}).encode(), SNAPSHOT, context)
        evaluated = [c for c in result['checks'] if c['outcome'] == 'EVALUATED']
        self.assertEqual(result['score']['value'], 100)
        self.assertLess(sum(c['max_points'] for c in evaluated), 100)
        self.assertTrue(any(group['score'] is None for group in result['subcategories']))
