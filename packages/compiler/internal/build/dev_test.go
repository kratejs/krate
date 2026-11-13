package build

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafeProjectPath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.tsx"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, ok := safeProjectPath(root, "a.tsx"); !ok {
		t.Error("valid relative path rejected")
	}
	// Diagnostics carry absolute paths; those inside the root must be accepted.
	absWithin := filepath.Join(root, "a.tsx")
	if _, ok := safeProjectPath(root, absWithin); !ok {
		t.Error("absolute path inside root rejected")
	}
	if _, ok := safeProjectPath(root, "../escape.tsx"); ok {
		t.Error("path escaping the root accepted")
	}
	// An absolute path outside the root must be rejected.
	outside := filepath.Join(t.TempDir(), "other.tsx")
	if err := os.WriteFile(outside, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, ok := safeProjectPath(root, outside); ok {
		t.Error("absolute path outside root accepted")
	}
	if _, ok := safeProjectPath(root, ""); ok {
		t.Error("empty path accepted")
	}
	if _, ok := safeProjectPath(root, "missing.tsx"); ok {
		t.Error("missing file accepted")
	}
	if _, ok := safeProjectPath(root, "."); ok {
		t.Error("directory path accepted")
	}
}
