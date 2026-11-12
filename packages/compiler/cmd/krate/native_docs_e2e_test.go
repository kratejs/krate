//go:build linux

package main

import (
	"bufio"
	"errors"
	"fmt"
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
	checkRefusesDev(t, exe)
	if os.Getenv("KRATE_NATIVE_GUI") == "1" {
		checkDocsInWindow(t, shell, filepath.Join(dist, "app"))
	}

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
		installed := filepath.Join(root, "usr", "lib", "krate-docs", "krate-docs")
		checkDocsPages(t, installed)
		checkRefusesDev(t, installed)
		if os.Getenv("KRATE_NATIVE_GUI") == "1" {
			checkDocsInWindow(t, shell, filepath.Join(filepath.Dir(installed), "app"))
		}
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
		if os.Getenv("KRATE_NATIVE_GUI") == "1" {
			checkDocsInWindow(t, shell, appImageAppDir(t, ai))
		}
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

// checkDocsInWindow opens the app in a real window (--dev, driven over stdin)
// and navigates it across several pages. A page that never comes back means
// the web process hung, as it did when the site's own viewTransitions setting
// reached the native build. It needs a display and WebKitGTK
// (KRATE_NATIVE_GUI=1, see the native docs for a rootless setup). The packaged
// executable takes no --dev (see checkRefusesDev), so the packaged app
// directory runs under the bare shell.
func checkDocsInWindow(t *testing.T, shell, appDir string) {
	t.Helper()
	exe := shell
	cmd := exec.Command(shell, "--dev", appDir)
	in, _ := cmd.StdinPipe()
	out, _ := cmd.StdoutPipe()
	cmd.Stderr = io.Discard
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	started := time.Now()
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting %s --dev: %v", exe, err)
	}
	defer func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
	}()
	lines := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	send := func(cmdline string, wait time.Duration) (string, bool) {
		fmt.Fprintln(in, cmdline)
		timeout := time.After(wait)
		for {
			select {
			case l, ok := <-lines:
				if !ok {
					return "", false
				}
				if strings.HasPrefix(l, "ok") || strings.HasPrefix(l, "error") {
					return l, true
				}
			case <-timeout:
				return "", false
			}
		}
	}
	var first string
	for time.Since(started) < 90*time.Second {
		if l, ok := send("query document.title", 20*time.Second); ok && strings.HasPrefix(l, "ok") {
			first = l
			break
		}
	}
	if first == "" {
		t.Fatal("the window never answered a query")
	}
	t.Logf("window: cold start to the first answered query %s (%s)", time.Since(started).Round(time.Millisecond), first)
	for _, path := range []string{"/docs/", "/docs/native/memory/", "/docs/core-concepts/routing/", "/blog/", "/"} {
		if _, ok := send(fmt.Sprintf("eval location.assign(%q)", path), 10*time.Second); !ok {
			t.Fatalf("navigating to %s: no reply", path)
		}
		var got string
		for i := 0; i < 20 && !strings.Contains(got, path); i++ {
			time.Sleep(500 * time.Millisecond)
			if l, ok := send("query location.pathname + ' ' + document.readyState", 8*time.Second); ok {
				got = l
			}
		}
		if !strings.Contains(got, path) || !strings.Contains(got, "complete") {
			t.Fatalf("after navigating to %s the page answered %q: the window did not load it", path, got)
		}
	}
	if l, ok := send("memory", 10*time.Second); ok {
		t.Logf("window memory: %.300s", l)
	}
}

// checkRefusesDev checks that a distributed executable (an app next to it)
// will not start with --dev, which would let whoever runs it drive the app's
// pages over the control channel under the app's identity.
func checkRefusesDev(t *testing.T, exe string) {
	t.Helper()
	cmd := exec.Command(exe, "--dev")
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 2 || !strings.Contains(string(out), "--dev is not available in a distributed app") {
		t.Errorf("%s --dev: %v\n%.400s", filepath.Base(exe), err, out)
	}
}

// appImageAppDir extracts an AppImage and returns the app directory inside it.
func appImageAppDir(t *testing.T, ai string) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command(ai, "--appimage-extract")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s --appimage-extract: %v\n%.400s", filepath.Base(ai), err, out)
	}
	var app string
	_ = filepath.Walk(filepath.Join(dir, "squashfs-root"), func(p string, fi os.FileInfo, err error) error {
		if err == nil && app == "" && !fi.IsDir() && fi.Name() == "app.json" {
			app = filepath.Dir(p)
		}
		return nil
	})
	if app == "" {
		t.Fatal("the AppImage holds no app.json")
	}
	return app
}
