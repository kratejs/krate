//go:build linux

package main

import (
	"bufio"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestNativeDocsSiteE2E is the acceptance check for Krate Native: the docs
// site (packages/web, TypeScript only) builds as a native app, packages as
// .deb, .rpm and .tar.gz (and an AppImage when appimagetool is installed), and
// the installed binaries serve its pages. It runs only with
// KRATE_E2E_DOCS=1 because it builds the shell and the whole site (about a
// minute), and it needs the JS packages built (pnpm build).
func TestNativeDocsSiteE2E(t *testing.T) {
	if os.Getenv("KRATE_E2E_DOCS") != "1" {
		t.Skip("set KRATE_E2E_DOCS=1 to run the docs-site acceptance test")
	}
	if runtime.GOOS != "linux" {
		t.Skip("packaging and launching are checked on Linux only")
	}
	repo, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	web := filepath.Join(repo, "packages", "web")
	if _, err := os.Stat(filepath.Join(repo, "packages", "core", "dist", "index.js")); err != nil {
		t.Skip("the JS packages are not built (pnpm build)")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is needed to build the shell")
	}

	out := t.TempDir()
	shell := filepath.Join(t.TempDir(), "krate-shell")
	b := exec.Command("go", "build", "-o", shell, "./cmd/krate-shell")
	b.Dir = filepath.Join(repo, "packages", "native")
	b.Env = append(os.Environ(), "CGO_ENABLED=0")
	if msg, err := b.CombinedOutput(); err != nil {
		t.Fatalf("building the shell: %v\n%s", err, msg)
	}

	start := time.Now()
	code, outp := runKrate(t, "native", "build", web, "--shell", shell, "--out", out)
	if code != 0 {
		t.Fatalf("native build exit %d:\n%s", code, outp)
	}
	t.Logf("native build of the docs site: %s", time.Since(start).Round(time.Millisecond))
	dist := filepath.Join(out, "linux-amd64")
	exe := filepath.Join(dist, "Krate Docs")
	for _, p := range []string{exe, filepath.Join(dist, "app", "app.json"), filepath.Join(dist, "app", "dist", "index.html")} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("native build did not write %s", p)
		}
	}
	// No precompressed siblings: the shell serves the bundle as it is.
	_ = filepath.Walk(filepath.Join(dist, "app", "dist"), func(p string, fi os.FileInfo, _ error) error {
		if ext := filepath.Ext(p); !fi.IsDir() && (ext == ".br" || ext == ".gz") {
			t.Errorf("the native bundle contains %s", p)
		}
		return nil
	})
	checkDocsPages(t, exe)

	formats := "deb,rpm,tar.gz"
	if _, err := exec.LookPath("appimagetool"); err == nil {
		if _, err := exec.LookPath("file"); err == nil {
			formats += ",appimage"
		}
	}
	code, outp = runKrate(t, "native", "package", web, "--out", out, "--format", formats)
	if code != 0 {
		t.Fatalf("native package exit %d:\n%s", code, outp)
	}
	pkgs := filepath.Join(out, "packages")
	size := func(name string) int64 {
		fi, err := os.Stat(filepath.Join(pkgs, name))
		if err != nil {
			t.Errorf("missing package %s", name)
			return 0
		}
		t.Logf("package %-40s %6.1f MB", name, float64(fi.Size())/1e6)
		return fi.Size()
	}

	// .deb: extract into a private root and run the installed binary.
	deb := "krate-docs_0.1.0_amd64.deb"
	size(deb)
	if _, err := exec.LookPath("dpkg-deb"); err == nil {
		root := filepath.Join(out, "deb-root")
		if msg, err := exec.Command("dpkg-deb", "-x", filepath.Join(pkgs, deb), root).CombinedOutput(); err != nil {
			t.Fatalf("dpkg-deb -x: %v\n%s", err, msg)
		}
		if _, err := os.Stat(filepath.Join(root, "usr", "share", "applications", "js.krate.docs.desktop")); err != nil {
			t.Errorf("the .deb has no desktop entry")
		}
		link, err := os.Readlink(filepath.Join(root, "usr", "bin", "krate-docs"))
		if err != nil || link != "../lib/krate-docs/krate-docs" {
			t.Errorf("usr/bin/krate-docs link = %q, %v", link, err)
		}
		checkDocsPages(t, filepath.Join(root, "usr", "lib", "krate-docs", "krate-docs"))
	} else {
		t.Log("dpkg-deb not found: .deb contents not checked")
	}

	// .rpm: lead magic, then the file list when the rpm tools exist.
	rpmName := "krate-docs-0.1.0-1.x86_64.rpm"
	size(rpmName)
	head := make([]byte, 4)
	if f, err := os.Open(filepath.Join(pkgs, rpmName)); err == nil {
		_, _ = io.ReadFull(f, head)
		f.Close()
		if string(head) != "\xed\xab\xee\xdb" {
			t.Errorf("the .rpm lead magic is % x", head)
		}
	}
	if _, err := exec.LookPath("rpm"); err == nil {
		msg, err := exec.Command("rpm", "-qpl", filepath.Join(pkgs, rpmName)).CombinedOutput()
		if err == nil && !strings.Contains(string(msg), "/usr/lib/krate-docs/krate-docs") {
			t.Errorf("rpm -qpl lacks the binary:\n%.400s", msg)
		}
	}
	size("krate-docs-0.1.0-linux-amd64.tar.gz")

	if strings.Contains(formats, "appimage") {
		ai := filepath.Join(pkgs, "krate-docs-0.1.0-linux-amd64.AppImage")
		size(filepath.Base(ai))
		checkDocsPages(t, ai, "--appimage-extract-and-run")
	} else {
		t.Log("appimagetool or file not found: AppImage not built")
	}
}

// checkDocsPages starts exe in browser mode (extra args come first), waits
// for its URL, loads several pages and stops it. The time from start to the
// URL is the cold start of the shell without a window.
func checkDocsPages(t *testing.T, exe string, pre ...string) {
	t.Helper()
	args := append(append([]string{}, pre...), "--browser")
	cmd := exec.Command(exe, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = io.Discard
	// Its own process group, so the stop below also reaches what an AppImage
	// runtime started.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	started := time.Now()
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting %s: %v", exe, err)
	}
	defer func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
	}()
	urlCh := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			if u, ok := strings.CutPrefix(sc.Text(), "url "); ok {
				urlCh <- u
				break
			}
		}
		_, _ = io.Copy(io.Discard, stdout)
	}()
	var u string
	select {
	case u = <-urlCh:
	case <-time.After(60 * time.Second):
		t.Fatalf("%s printed no URL within 60s", exe)
	}
	cold := time.Since(started)
	base, tok, _ := strings.Cut(u, "/?krate_token=")
	pages := map[string]string{
		"/":                            "Krate",
		"/docs/":                       "Docs",
		"/docs/native/memory/":         "Memory",
		"/docs/native/":                "Native",
		"/docs/core-concepts/routing/": "Routing",
		"/blog/":                       "Blog",
	}
	for path, title := range pages {
		req, _ := http.NewRequest("GET", base+path, nil)
		req.Header.Set("X-Krate-Token", tok)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Errorf("%s%s: %v", filepath.Base(exe), path, err)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || !strings.Contains(string(body), "<title>") || !strings.Contains(string(body), title) {
			t.Errorf("%s: status %d, %d bytes, no %q", path, resp.StatusCode, len(body), title)
		}
	}
	t.Logf("%s: %d pages served; cold start to the URL %s", filepath.Base(exe), len(pages), cold.Round(time.Millisecond))
}
