package pickerstate

import (
	"os"
	"testing"
)

func TestNewAndCleanup(t *testing.T) {
	path, err := New()
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("session file should exist after New: %v", err)
	}
	Cleanup(path)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("session file should be removed after Cleanup, got err=%v", err)
	}
	// Cleanup is idempotent: calling on a missing file shouldn't panic
	// or return.
	Cleanup(path)
}

func TestSetAndReadTagFilter(t *testing.T) {
	path, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer Cleanup(path)

	// Initially empty.
	tags, err := TagFilter(path)
	if err != nil {
		t.Fatalf("TagFilter: %v", err)
	}
	if len(tags) != 0 {
		t.Errorf("expected empty filter initially, got %v", tags)
	}

	// Set a couple.
	if err := SetTagFilter(path, []string{"git", "stash"}); err != nil {
		t.Fatalf("SetTagFilter: %v", err)
	}
	tags, _ = TagFilter(path)
	if !equalSlice(tags, []string{"git", "stash"}) {
		t.Errorf("after set: got %v, want [git stash]", tags)
	}

	// Replace with a different set.
	if err := SetTagFilter(path, []string{"docker"}); err != nil {
		t.Fatalf("SetTagFilter (replace): %v", err)
	}
	tags, _ = TagFilter(path)
	if !equalSlice(tags, []string{"docker"}) {
		t.Errorf("after replace: got %v, want [docker]", tags)
	}

	// Clear via empty slice.
	if err := SetTagFilter(path, nil); err != nil {
		t.Fatalf("SetTagFilter (clear): %v", err)
	}
	tags, _ = TagFilter(path)
	if len(tags) != 0 {
		t.Errorf("after clear: got %v, want empty", tags)
	}
}

func TestAddTagFilter(t *testing.T) {
	path, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer Cleanup(path)

	if err := AddTagFilter(path, "git"); err != nil {
		t.Fatalf("AddTagFilter: %v", err)
	}
	if err := AddTagFilter(path, "stash"); err != nil {
		t.Fatalf("AddTagFilter: %v", err)
	}
	// Adding a duplicate is a no-op.
	if err := AddTagFilter(path, "git"); err != nil {
		t.Fatalf("AddTagFilter dup: %v", err)
	}
	tags, _ := TagFilter(path)
	if !equalSlice(tags, []string{"git", "stash"}) {
		t.Errorf("after adds: got %v, want [git stash]", tags)
	}
}

func TestClearTagFilter(t *testing.T) {
	path, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer Cleanup(path)

	_ = SetTagFilter(path, []string{"a", "b", "c"})
	if err := ClearTagFilter(path); err != nil {
		t.Fatalf("ClearTagFilter: %v", err)
	}
	tags, _ := TagFilter(path)
	if len(tags) != 0 {
		t.Errorf("after clear: got %v, want empty", tags)
	}
}

func TestTagFilterMissingFile(t *testing.T) {
	// Reading from a nonexistent path should return nil, no error.
	tags, err := TagFilter("/nonexistent/path/that/should/not/exist")
	if err != nil {
		t.Errorf("expected nil error for missing file, got %v", err)
	}
	if tags != nil {
		t.Errorf("expected nil tags, got %v", tags)
	}
}

func TestTagFilterEmptyPath(t *testing.T) {
	tags, err := TagFilter("")
	if err != nil {
		t.Errorf("expected nil error for empty path, got %v", err)
	}
	if tags != nil {
		t.Errorf("expected nil tags, got %v", tags)
	}
}

func equalSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
