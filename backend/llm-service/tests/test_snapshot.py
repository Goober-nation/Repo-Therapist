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

    def test_oversized_file_keeps_its_beginning_without_split_characters(self):
        store = FakeStore()
        store.objects["repos/123/README.md"] = "# Проект\n".encode() + "я".encode() * 20
        # "# Проект\n" is 15 bytes; a 16-byte cap splits the next two-byte "я".
        result = load_snapshot(store, "repos/123", {"README.md"}, max_file_bytes=16)
        self.assertEqual(result.files["README.md"], "# Проект\n")
        result = load_snapshot(store, "repos/123", {"README.md"}, max_file_bytes=17)
        self.assertEqual(result.files["README.md"], "# Проект\nя")

    def test_budget_skips_optional_files_but_always_reads_required(self):
        store = FakeStore()
        store.objects["repos/123/LICENSE"] = b"MIT " * 10
        store.objects["repos/123/docs/a.md"] = b"a" * 30
        store.objects["repos/123/docs/b.md"] = b"b" * 30
        result = load_snapshot(store, "repos/123", {"LICENSE", "docs/a.md", "docs/b.md"},
                               max_total_bytes=75, required={"LICENSE"})
        self.assertEqual(set(result.files), {"LICENSE", "docs/a.md"})

    def test_non_utf8_optional_is_skipped_and_required_is_kept_unreadable(self):
        store = FakeStore()
        store.objects["repos/123/LICENSE"] = "Лицензия".encode("cp1251")
        store.objects["repos/123/docs/a.md"] = "Документ".encode("cp1251")
        result = load_snapshot(store, "repos/123", {"LICENSE", "docs/a.md"},
                               required={"LICENSE"})
        self.assertEqual(set(result.files), {"LICENSE"})
        self.assertEqual(result.unreadable, {"LICENSE"})
        self.assertTrue(result.files["LICENSE"].strip())


if __name__ == "__main__":
    unittest.main()
