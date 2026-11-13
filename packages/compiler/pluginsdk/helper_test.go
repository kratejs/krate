package plug

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathHelpers(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := ResolveFile(root, "./a.txt")
	if err != nil {
		t.Fatalf("ResolveFile: %v", err)
	}
	if want := filepath.Join(root, "a.txt"); got != want {
		t.Errorf("ResolveFile = %q, want %q", got, want)
	}

	content, err := ReadFile(root, "./a.txt")
	if err != nil || content != "hi" {
		t.Fatalf("ReadFile = %q (err %v)", content, err)
	}

	if _, err := ReadFile(root, "../escape.txt"); err == nil {
		t.Fatal("expected traversal error")
	}

	if err := WriteFileToRoot(root, "public/b.txt", []byte("b")); err != nil {
		t.Fatalf("WriteFileToRoot: %v", err)
	}
	if err := WriteFileToRoot(root, "../escape.txt", []byte("no")); err == nil {
		t.Fatal("expected traversal error from WriteFileToRoot")
	}
}
