import unittest

from llm_service.snapshot import SnapshotError, load_snapshot


class FakeStore:
    def __init__(self):
        self.objects = {
            "repos/123/README.md": b"# Demo\n\nDescription\n",
            "repos/123/_meta/export.json": b"{}",
        }
        self.mutate_on_read = False
        self.mutate_on_second_list = False
        self.list_count = 0

    def list_objects(self, prefix):
        self.list_count += 1
        if self.mutate_on_second_list and self.list_count == 2:
            self.objects["repos/123/new.txt"] = b"new"
        return {key: (len(value), str(hash(value))) for key, value in self.objects.items()
                if key.startswith(prefix)}

    def read_object(self, key):
        value = self.objects[key]
        if self.mutate_on_read:
            self.objects[key] = value + b"changed"
        return value


class SnapshotTests(unittest.TestCase):
    def test_loads_selected_text_but_lists_entire_prefix(self):
        result = load_snapshot(FakeStore(), "repos/123", {"README.md"})
        self.assertEqual(result.all_paths, {"README.md"})
        self.assertIn("Description", result.files["README.md"])
        self.assertEqual(len(result.hashes["README.md"]), 64)

    def test_rejects_changed_content(self):
        store = FakeStore()
        store.mutate_on_read = True
        with self.assertRaisesRegex(SnapshotError, "content changed"):
            load_snapshot(store, "repos/123", {"README.md"})

    def test_rejects_changed_listing(self):
        store = FakeStore()
        store.mutate_on_second_list = True
        with self.assertRaisesRegex(SnapshotError, "listing changed"):
            load_snapshot(store, "repos/123", {"README.md"})

    def test_rejects_missing_prefix_instead_of_claiming_missing_files(self):
        with self.assertRaisesRegex(SnapshotError, "prefix not found"):
            load_snapshot(FakeStore(), "repos/unknown", set())


if __name__ == "__main__":
    unittest.main()
