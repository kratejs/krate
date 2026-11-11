package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// installedKrate copies the test binary to <bin>/krate, the way an npm
// platform package holds the compiler, and puts a prebuilt shell next to it.
// It returns a function that runs that copy as the krate command with a PATH
// that has no go command (an empty directory), so anything that needs Go
// fails.
func installedKrate(t *testing.T) func(args ...string) (int, string) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "pkg", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	self, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	krate := filepath.Join(bin, "krate")
	if err := os.WriteFile(krate, self, 0o755); err != nil {
		t.Fatal(err)
	}
	shell := "krate-shell"
	if runtime.GOOS == "windows" {
		shell += ".exe"
	}
	if err := os.WriteFile(filepath.Join(bin, shell), []byte("#!/bin/sh\necho prebuilt shell\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	emptyPath := t.TempDir()
	return func(args ...string) (int, string) {
		t.Helper()
		c := exec.Command(krate, "-test.run=^TestKrateMainHelper$")
		c.Env = []string{
			"PATH=" + emptyPath, "HOME=" + t.TempDir(), "TMPDIR=" + os.TempDir(),
			"KRATE_TEST_HELPER_MAIN=1", "KRATE_TEST_HELPER_ARGS=" + strings.Join(args, "\n"),
		}
		out, err := c.CombinedOutput()
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode(), string(out)
		}
		if err != nil {
			t.Fatalf("running krate %v: %v", args, err)
		}
		return 0, string(out)
	}
}

func tarGzNames(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(zr)
	names := map[string]string{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return names
		}
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if h.Typeflag == tar.TypeReg {
			if _, err := io.Copy(&buf, tr); err != nil {
				t.Fatal(err)
			}
		}
		names[h.Name] = buf.String()
	}
}

// TestNativePrebuiltShellWithoutGo builds a TypeScript-only app with a krate
// that has the prebuilt shell next to it and no go command on PATH, and
// packages the result: the shell becomes <productName> with app/ beside it.
func TestNativePrebuiltShellWithoutGo(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("packages the Linux formats")
	}
	run := installedKrate(t)
	dir := filepath.Join(t.TempDir(), "hello-app")
	if code, out := runKrate(t, "native", "init", dir); code != 0 {
		t.Fatalf("init exit %d:\n%s", code, out)
	}
	for name, content := range nativeDeps {
		writeTestFile(t, filepath.Join(dir, filepath.FromSlash(name)), content)
	}
	target := runtime.GOOS + "-" + runtime.GOARCH
	dist := filepath.Join(dir, "dist-native", target)
	check := func(label string) {
		t.Helper()
		exe := filepath.Join(dist, "Hello App")
		st, err := os.Stat(exe)
		if err != nil || st.Mode()&0o111 == 0 {
			t.Fatalf("%s: no executable %s: %v", label, exe, err)
		}
		if data, _ := os.ReadFile(exe); !strings.Contains(string(data), "prebuilt shell") {
			t.Errorf("%s: %s is not the shell", label, exe)
		}
		for _, f := range []string{"app/app.json", "app/dist/index.html", "app/host/host.js"} {
			if _, err := os.Stat(filepath.Join(dist, filepath.FromSlash(f))); err != nil {
				t.Errorf("%s: missing %s", label, f)
			}
		}
	}

	// A plain build finds the shell next to the krate executable.
	code, out := run("native", "build", dir)
	if code != 0 {
		t.Fatalf("build exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "distributable") {
		t.Errorf("build output does not mention the distributable:\n%s", out)
	}
	check("build")

	// A release build without Go falls back to the same layout (compile: auto).
	os.RemoveAll(filepath.Join(dir, "dist-native"))
	code, out = run("native", "build", dir, "--release")
	if code != 0 {
		t.Fatalf("release exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "prebuilt-shell layout") {
		t.Errorf("release output:\n%s", out)
	}
	check("release")
	report, _ := os.ReadFile(filepath.Join(dir, "dist-native", "report.json"))
	for _, want := range []string{`"layout": "app-dir"`, `"binary": "` + target + `/Hello App"`} {
		if !strings.Contains(string(report), want) {
			t.Errorf("report.json lacks %s:\n%s", want, report)
		}
	}

	code, out = run("native", "package", dir, "--format", "tar.gz,deb")
	if code != 0 {
		t.Fatalf("package exit %d:\n%s", code, out)
	}
	pkgs := filepath.Join(dir, "dist-native", "packages")
	names := tarGzNames(t, filepath.Join(pkgs, "hello-app-0.1.0-"+target+".tar.gz"))
	var sawExe, sawManifest bool
	for n, body := range names {
		if n == "hello-app" {
			sawExe = strings.Contains(body, "prebuilt shell")
		}
		if strings.HasSuffix(n, "app/app.json") {
			sawManifest = strings.Contains(body, "com.example.helloapp")
		}
	}
	if !sawExe || !sawManifest {
		t.Errorf("tar.gz lacks the shell or app/app.json (exe %v, manifest %v): %v", sawExe, sawManifest, keys(names))
	}
	debs, _ := filepath.Glob(filepath.Join(pkgs, "*.deb"))
	if len(debs) != 1 {
		t.Fatalf("deb files = %v\n%s", debs, out)
	}
	raw, _ := os.ReadFile(debs[0])
	if !bytes.HasPrefix(raw, []byte("!<arch>\n")) || !bytes.Contains(raw, []byte("data.tar")) {
		t.Fatalf("%s is not a deb archive", debs[0])
	}
	data := debDataTar(t, raw)
	if !strings.Contains(strings.Join(data, "\n"), "/app/app.json") {
		t.Errorf("deb payload lacks app/app.json: %v", data)
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// debDataTar lists the file names of the data.tar.gz member of a deb.
func debDataTar(t *testing.T, raw []byte) []string {
	t.Helper()
	rest := raw[len("!<arch>\n"):]
	for len(rest) >= 60 {
		name := strings.TrimSpace(string(rest[:16]))
		var size int
		for _, c := range strings.TrimSpace(string(rest[48:58])) {
			size = size*10 + int(c-'0')
		}
		body := rest[60 : 60+size]
		rest = rest[60+size:]
		if size%2 == 1 && len(rest) > 0 {
			rest = rest[1:]
		}
		if !strings.HasPrefix(name, "data.tar") {
			continue
		}
		zr, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		tr := tar.NewReader(zr)
		var names []string
		for {
			h, err := tr.Next()
			if err != nil {
				return names
			}
			names = append(names, h.Name)
		}
	}
	t.Fatal("no data.tar member")
	return nil
}

// TestNativeDoctorCommand runs krate native doctor against a fake shell that
// answers --doctor: a healthy machine exits 0, a missing shell exits 1.
func TestNativeDoctorCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake shell is a shell script")
	}
	dir := nativeProject(t, map[string]string{
		"krate.config.json":   `{"native":{"appId":"com.acme.app"}}`,
		"src/pages/index.tsx": nativePage,
	})
	shell := filepath.Join(t.TempDir(), "krate-shell")
	report := `{"schema":1,"shell":"krate-shell","os":"` + runtime.GOOS + `","arch":"` + runtime.GOARCH +
		`","webview":{"name":"TestView","present":true,"version":"9.9.9","minVersion":"1.0","ok":true},"display":{"available":true,"detail":"test display"}}`
	if err := os.WriteFile(shell, []byte("#!/bin/sh\n[ \"$1\" = --doctor ] && echo '"+report+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("KRATE_SHELL", shell)
	code, out := runKrate(t, "native", "doctor", dir)
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	for _, want := range []string{"Krate Native doctor", "krate-shell", shell, "TestView", "9.9.9", "test display", "Everything this project needs"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	code, out = runKrate(t, "native", "doctor", dir, "--json")
	if code != 0 || !strings.Contains(out, `"group": "webview"`) {
		t.Errorf("--json: exit %d:\n%s", code, out)
	}

	// A shell that is not there is a required item.
	t.Setenv("KRATE_SHELL", filepath.Join(t.TempDir(), "gone"))
	code, out = runKrate(t, "native", "doctor", dir)
	if code != 1 || !strings.Contains(out, "MISSING") || !strings.Contains(out, "does not exist") {
		t.Errorf("missing shell: exit %d:\n%s", code, out)
	}
}
