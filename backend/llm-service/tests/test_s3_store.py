import io
import unittest

from llm_service.context import prepare_documentation_context
from llm_service.s3_store import S3Config, S3Store


class FakePages:
    def __init__(self, objects):
        self.objects = objects
        self.calls = []

    def paginate(self, **kwargs):
        self.calls.append(kwargs)
        entries = [{"Key": key, "Size": len(value), "ETag": str(hash(value))}
                   for key, value in self.objects.items() if key.startswith(kwargs["Prefix"])]
        yield {"Contents": entries[:1]}
        yield {"Contents": entries[1:]}


class FakeS3:
    def __init__(self):
        self.objects = {"repos/123/README.md": b"# Sample\n\nDescription.",
                        "repos/123/_meta/export.json": b"{}",
                        "repos/1234/README.md": b"not ours"}
        self.pages = FakePages(self.objects)
        self.reads = []

    def get_paginator(self, operation):
        assert operation == "list_objects_v2"
        return self.pages

    def get_object(self, *, Bucket, Key):
        self.reads.append((Bucket, Key))
        return {"Body": io.BytesIO(self.objects[Key])}


class S3StoreTests(unittest.TestCase):
    def test_prefix_pagination_and_snapshot_read(self):
        client = FakeS3()
        store = S3Store(S3Config(access_key="access", secret_key="secret"), client)
        snapshot, context = prepare_documentation_context(store, "repos/123")
        self.assertEqual(snapshot.all_paths, {"README.md"})
        self.assertEqual(context.selected_file_count, 1)
        self.assertEqual(client.pages.calls, [
            {"Bucket": "repo-content", "Prefix": "repos/123/"},
            {"Bucket": "repo-content", "Prefix": "repos/123/"},
        ])
        self.assertEqual(client.reads, [("repo-content", "repos/123/README.md")] * 2)

    def test_credentials_are_required(self):
        with self.assertRaises(ValueError):
            S3Config()


if __name__ == "__main__":
    unittest.main()
