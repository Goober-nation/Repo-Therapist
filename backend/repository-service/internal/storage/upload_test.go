package storage

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func entryOf(t *testing.T, dir, name string) fs.DirEntry {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range entries {
		if e.Name() == name {
			return e
		}
	}

	t.Fatalf("no entry %s", name)

	return nil
}

func TestOpenUploadable(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "real.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	for name, target := range map[string]string{"dangling": "missing", "tofile": "real.txt", "todir": "sub"} {
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Skip("symlinks unavailable")
		}
	}

	cases := []struct {
		name     string
		wantFile bool
	}{{"real.txt", true}, {"tofile", true}, {"dangling", false}, {"todir", false}}
	for _, c := range cases {
		f, info, err := openUploadable(filepath.Join(dir, c.name), entryOf(t, dir, c.name))
		if err != nil {
			t.Errorf("%s: unexpected error %v", c.name, err)

			continue
		}

		if (f != nil) != c.wantFile {
			t.Errorf("%s: file opened = %v, want %v", c.name, f != nil, c.wantFile)
		}

		if f != nil {
			if info.Size() != 2 {
				t.Errorf("%s: size = %d, want 2", c.name, info.Size())
			}

			f.Close()
		}
	}

	// A regular file that cannot be opened is still an error, not a silent skip.
	if _, _, err := openUploadable(filepath.Join(dir, "gone.txt"), entryOf(t, dir, "real.txt")); err == nil {
		t.Error("an unopenable regular file should be an error")
	}
}
