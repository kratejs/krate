package gitinfo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("git %v failed: %v: %s", args, err, out)
	}
}

func TestGitRootIsCachedPerRoot(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	nonRepo := t.TempDir() // queried FIRST — must not poison the repo root
	repo := t.TempDir()
	git(t, repo, "init")
	if err := os.MkdirAll(filepath.Join(repo, "docs"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "docs", "a.md"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "init")

	if Available(nonRepo) {
		t.Fatal("non-repo root reported as a git repository")
	}
	if !Available(repo) {
		t.Fatal("git repo root not detected — root cache is leaking across projects")
	}
	rel := filepath.Join("docs", "a.md")
	if got := LastCommit(repo, rel); got == "" {
		t.Fatal("LastCommit returned empty for a committed file (relative path)")
	}
	// Absolute paths must resolve to the same commit, even though git's
	// resolved top-level can differ from root (symlinked temp dirs on macOS,
	// short/case-variant paths on Windows).
	absCommit := LastCommit(repo, filepath.Join(repo, rel))
	if absCommit == "" {
		t.Fatal("LastCommit returned empty for a committed file (absolute path)")
	}
	if absCommit != LastCommit(repo, rel) {
		t.Fatalf("absolute vs relative LastCommit disagree: %q != %q", absCommit, LastCommit(repo, rel))
	}
	if got := LastAuthors(repo, filepath.Join(repo, rel), 3); len(got) == 0 {
		t.Fatal("LastAuthors returned none for a committed file")
	}
	if got := LastCommit(nonRepo, "a.md"); got != "" {
		t.Fatalf("LastCommit for a non-repo root = %q, want empty", got)
	}
}
