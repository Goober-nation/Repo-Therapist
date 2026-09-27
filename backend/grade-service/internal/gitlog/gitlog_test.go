package gitlog

import (
	"reflect"
	"testing"
)

// parseGitLog must carry %P parents: [] for a root commit, 2 hashes for a merge (which has no
// numstat lines, so parents are the only way to tell it from an empty commit).
func TestParseGitLogParents(t *testing.T) {
	out := recordSep + "c3" + unitSep + "Ann" + unitSep + "ann@x.dev" + unitSep + "2026-09-20T10:00:00+03:00" + unitSep + "c1 c2" + unitSep + "Merge branch" + "\n" +
		recordSep + "c2" + unitSep + "Ann" + unitSep + "ann@x.dev" + unitSep + "2026-09-19T10:00:00+03:00" + unitSep + "c1" + unitSep + "Add main" + "\n\n1\t0\tmain.go\n" +
		recordSep + "c1" + unitSep + "Ann" + unitSep + "ann@x.dev" + unitSep + "2026-09-18T10:00:00+03:00" + unitSep + "" + unitSep + "Initial" + "\n"
	commits, err := parseGitLog(out)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{"c3": {"c1", "c2"}, "c2": {"c1"}, "c1": {}}
	if len(commits) != 3 {
		t.Fatalf("got %d commits, want 3", len(commits))
	}
	for _, c := range commits {
		if !reflect.DeepEqual(c.Parents, want[c.Hash]) {
			t.Errorf("%s parents = %#v, want %#v", c.Hash, c.Parents, want[c.Hash])
		}
	}
	if commits[1].Subject != "Add main" || len(commits[1].FilesChanged) != 1 {
		t.Errorf("c2 = %+v, want subject and one file", commits[1])
	}
}
