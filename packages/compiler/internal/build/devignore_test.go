package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/ast"
)

func gitInit(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		requireE2E(t, "git is not installed")
	}
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
}

func TestGitIgnoresFollowsTheIgnoreRules(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	gitInit(t, root)
	writeProjectFile(t, root, ".gitignore", "data/\n*.log\n")
	writeProjectFile(t, root, "data/media/status.json", "{}")
	writeProjectFile(t, root, "src/pages/index.tsx", devPage("home"))
	writeProjectFile(t, root, "src/app.log", "")
	g := newGitIgnores(root)
	cases := []struct {
		path  string
		isDir bool
		want  bool
	}{
		{"data/media/status.json", false, true},
		{"data/media/seg/0001.ts", false, true}, // not written yet: still ignored
		{"data/media", true, true},
		{"src/pages/index.tsx", false, false},
		{"src/app.log", false, true},
		{"src", true, false},
	}
	for _, c := range cases {
		if got := g.ignored(filepath.Join(root, filepath.FromSlash(c.path)), c.isDir); got != c.want {
			t.Errorf("ignored(%s) = %v, want %v", c.path, got, c.want)
		}
	}
	// A .gitignore edit applies once the cache is reset.
	writeProjectFile(t, root, ".gitignore", "*.log\n")
	g.reset()
	if g.ignored(filepath.Join(root, "data", "media", "status.json"), false) {
		t.Error("data/ is no longer ignored after the .gitignore edit")
	}
	// Outside a work tree nothing is ignored.
	plain := t.TempDir()
	writeProjectFile(t, plain, ".gitignore", "data/\n")
	if newGitIgnores(plain).ignored(filepath.Join(plain, "data", "x.json"), false) {
		t.Error("a directory outside git ignored a path")
	}
}

// An API route or a background job that writes runtime data into a
// git-ignored directory of the project (a .json progress file, video
// segments with a .ts extension) must not rebuild the site: in a browser
// with live reload every rebuild reloads the page, the page calls the API
// again and the API writes again, so the dev server rebuilt every few seconds.
func TestDevWatchIgnoresRuntimeDataInGitIgnoredDirs(t *testing.T) {
	t.Parallel()
	root, _, events := startDevWatch(t, map[string]string{
		".gitignore":          "data/\n",
		"src/pages/index.tsx": devPage("home"),
	}, nil)
	gitInit(t, root)
	drain := func() {
		for {
			select {
			case <-events:
			default:
				return
			}
		}
	}
	reloads := func(within time.Duration) int {
		n := 0
		deadline := time.After(within)
		for {
			select {
			case ev := <-events:
				if ev.Type == "reload" {
					n++
				}
			case <-deadline:
				return n
			}
		}
	}
	drain()
	for i := 0; i < 3; i++ {
		writeProjectFile(t, root, "data/media/status.json", `{"progress":`+string(rune('0'+i))+`}`)
		writeProjectFile(t, root, "data/media/hls/000"+string(rune('0'+i))+".ts", "segment")
		time.Sleep(100 * time.Millisecond)
	}
	if err := os.RemoveAll(filepath.Join(root, "data", "media", "hls")); err != nil {
		t.Fatal(err)
	}
	if n := reloads(1500 * time.Millisecond); n != 0 {
		t.Fatalf("writing runtime data under a git-ignored directory rebuilt the site %d times", n)
	}
	// The watcher still rebuilds for a source edit.
	writeProjectFile(t, root, "src/pages/index.tsx", devPage("edited"))
	if n := reloads(15 * time.Second); n == 0 {
		t.Fatal("a page edit after the ignored writes did not rebuild")
	}
}

// A dev server rebuilds for hours: the state it keeps per parsed program must
// not keep every program it ever parsed. A page entry is parsed again on every
// build and an edited module again after each edit; before, the maps keyed by
// program held each of those ASTs for the life of the process (about 100 MB
// per full rebuild of a mid-sized app).
func TestDevRebuildsForgetTheProgramsOfEarlierBuilds(t *testing.T) {
	t.Parallel()
	b, root := scProject(t, map[string]string{
		"src/components/Card.tsx": `export function Card({ t }: { t: string }) { return <div class="card">{t}</div>; }`,
		"src/pages/index.tsx":     `import { Card } from "../components/Card"; export default function Home() { return <main><Card t="home" /></main>; }`,
		"src/pages/about.tsx":     `import { Card } from "../components/Card"; export default function About() { return <main><Card t="about" /></main>; }`,
	})
	b.DevMode = true
	UseDevOutDir(root, b.Cfg)
	count := func() int {
		n := 0
		b.progLocks.Range(func(k, _ any) bool {
			if _, ok := k.(*ast.Program); ok {
				n++
			}
			return true
		})
		return n
	}
	var after []int
	for i := 0; i < 6; i++ {
		if i%2 == 1 {
			// An edit: the module is parsed again, its old program is stale.
			writeProjectFile(t, root, "src/components/Card.tsx", `export function Card({ t }: { t: string }) { return <div class="card v`+string(rune('0'+i))+`">{t}</div>; }`)
			past := time.Now().Add(-time.Minute + time.Duration(i)*time.Second)
			_ = os.Chtimes(filepath.Join(root, "src", "components", "Card.tsx"), past, past)
		}
		if err := b.BuildAll(); err != nil {
			t.Fatalf("BuildAll %d: %v", i, err)
		}
		after = append(after, count())
	}
	if after[0] == 0 {
		t.Fatal("no program state recorded: the test does not see the transforms")
	}
	for i := 2; i < len(after); i++ {
		if after[i] > after[1] {
			t.Fatalf("program state grows with every rebuild: %v", after)
		}
	}
}
