// Package golden compares test output against a checked-in file, and rewrites it
// with -update. The diff of a golden file is the review.
package golden

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files in place of comparing")

// Updating reports whether this run should rewrite goldens. The -update flag only
// exists in packages that import this one, so `make golden` sets UPDATE_GOLDEN=1
// instead and a whole-tree run works either way.
func Updating() bool { return *update || os.Getenv("UPDATE_GOLDEN") != "" }

// JSON compares v against the golden file at path, pretty-printed so the diff is
// readable line by line.
func JSON(t *testing.T, path string, v any) {
	t.Helper()
	got, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got = append(got, '\n')
	compare(t, path, got)
}

// Text compares a rendered transcript against the golden file at path.
func Text(t *testing.T, path, got string) {
	t.Helper()
	compare(t, path, []byte(got))
}

func compare(t *testing.T, path string, got []byte) {
	t.Helper()
	if Updating() {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v\nrun: make golden", path, err)
	}
	if !bytes.Equal(want, got) {
		t.Errorf("%s does not match.\n--- want ---\n%s\n--- got ---\n%s\nrun: make golden", path, want, got)
	}
}
