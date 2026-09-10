// Package golden compares test output against a checked-in file, and rewrites it
// with -update. The diff of a golden file is the review.
package golden

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files instead of comparing")

// Updating reports whether -update was passed, for tests that must generate
// several files at once.
func Updating() bool { return *update }

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
func Text(t *testing.T, path string, got string) {
	t.Helper()
	compare(t, path, []byte(got))
}

func compare(t *testing.T, path string, got []byte) {
	t.Helper()
	if *update {
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
		t.Fatalf("%s: %v\nrun: go test ./... -update", path, err)
	}
	if string(want) != string(got) {
		t.Errorf("%s does not match.\n--- want ---\n%s\n--- got ---\n%s\nrun: go test ./... -update", path, want, got)
	}
}
