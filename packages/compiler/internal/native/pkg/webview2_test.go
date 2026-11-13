package pkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/native/toastscheme"
)

func nsisFor(t *testing.T, m Metadata) string {
	t.Helper()
	return NSISScript(m, Payload{OS: "windows", Arch: "amd64"}, `C:\stage`, `C:\out\setup.exe`)
}

func TestNSISWebView2Download(t *testing.T) {
	m := testMeta(t)
	s := nsisFor(t, m)
	for _, want := range []string{
		"Function HasWebView2", "Function EnsureWebView2", "  Call EnsureWebView2",
		// The documented registry locations: per machine (the 32-bit view of
		// this installer is WOW6432Node on 64-bit Windows) and per user.
		"SetRegView 32", `ReadRegStr $1 HKLM "SOFTWARE\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"`,
		`ReadRegStr $1 HKCU "Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"`,
		`"0.0.0.0"`, `NSISdl::download "https://go.microsoft.com/fwlink/p/?LinkId=2124703"`, "powershell.exe",
		`ExecWait '"$2" /silent /install' $1`, `!include "LogicLib.nsh"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("download mode lacks %q", want)
		}
	}
	if strings.Contains(s, "/oname=") {
		t.Error("download mode embeds a file")
	}
	// Check order: the runtime is looked for before anything is downloaded,
	// and the check runs before files are copied.
	if strings.Index(s, "Call HasWebView2") > strings.Index(s, "NSISdl::download") {
		t.Error("the download comes before the registry check")
	}
}

func TestNSISWebView2Embed(t *testing.T) {
	m := testMeta(t)
	m.WebView2, m.WebView2Path = "embed", `C:\vendor\MicrosoftEdgeWebview2Setup.exe`
	s := nsisFor(t, m)
	if !strings.Contains(s, `File "/oname=$PLUGINSDIR\MicrosoftEdgeWebview2Setup.exe" "C:\vendor\MicrosoftEdgeWebview2Setup.exe"`) {
		t.Errorf("embed mode does not add the bootstrapper:\n%s", s)
	}
	if strings.Contains(s, "NSISdl::download") {
		t.Error("embed mode downloads")
	}
	if !strings.Contains(s, "  Call EnsureWebView2") || !strings.Contains(s, "/silent /install") {
		t.Error("embed mode does not run the bootstrapper")
	}
}

func TestNSISWebView2NoneAndFixed(t *testing.T) {
	for _, mode := range []string{"none", "fixed"} {
		m := testMeta(t)
		m.WebView2, m.WebView2Path = mode, `C:\x`
		s := nsisFor(t, m)
		if strings.Contains(s, "Call EnsureWebView2") || strings.Contains(s, "NSISdl") || strings.Contains(s, "Function EnsureWebView2") {
			t.Errorf("%s mode installs the runtime:\n%s", mode, s)
		}
	}
}

func TestNSISRegistersTheToastScheme(t *testing.T) {
	m := testMeta(t)
	s := nsisFor(t, m)
	scheme := toastscheme.Scheme(m.AppID)
	for _, want := range []string{
		`WriteRegStr HKLM "Software\Classes\` + scheme + `" "URL Protocol" ""`,
		`WriteRegStr HKLM "Software\Classes\` + scheme + `\shell\open\command" "" '"$INSTDIR\${APPEXE}" "%1"'`,
		`DeleteRegKey HKLM "Software\Classes\` + scheme + `"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("nsi lacks %q", want)
		}
	}
}

func TestMSIXDeclaresTheToastScheme(t *testing.T) {
	m := testMeta(t)
	mf := string(AppxManifest(m, Payload{OS: "windows", Arch: "amd64"}))
	scheme := toastscheme.Scheme(m.AppID)
	if !strings.Contains(mf, `<uap3:Protocol Name="`+scheme+`" Parameters="&quot;%1&quot;">`) {
		t.Errorf("manifest lacks the toast scheme:\n%s", mf)
	}
	// An app with no deep links still gets it.
	m.DeepLinks = nil
	if mf = string(AppxManifest(m, Payload{OS: "windows", Arch: "arm64"})); !strings.Contains(mf, scheme) || !strings.Contains(mf, `ProcessorArchitecture="arm64"`) {
		t.Errorf("manifest without deep links:\n%s", mf)
	}
}

func TestAddWebView2(t *testing.T) {
	dir := t.TempDir()
	m := testMeta(t)

	// download and none add nothing and need no path.
	for _, mode := range []string{"", "download", "none"} {
		m.WebView2 = mode
		tr := newTree()
		if err := addWebView2(m, tr, "App"); err != nil || len(tr.m) != 0 {
			t.Errorf("mode %q: err %v, entries %d", mode, err, len(tr.m))
		}
	}

	// fixed copies the runtime folder under WebView2Runtime.
	rt := filepath.Join(dir, "runtime")
	if err := os.MkdirAll(filepath.Join(rt, "EBWebView", "x64"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"msedgewebview2.exe", filepath.Join("EBWebView", "x64", "EmbeddedBrowserWebView.dll")} {
		if err := os.WriteFile(filepath.Join(rt, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m.WebView2, m.WebView2Path = "fixed", rt
	tr := newTree()
	if err := addWebView2(m, tr, "App"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"App/WebView2Runtime/msedgewebview2.exe", "App/WebView2Runtime/EBWebView/x64/EmbeddedBrowserWebView.dll"} {
		if tr.m[want] == nil {
			t.Errorf("tree lacks %s (has %d entries)", want, len(tr.m))
		}
	}
	m.WebView2Path = dir // not a runtime folder
	if err := addWebView2(m, newTree(), "App"); err == nil || !strings.Contains(err.Error(), "msedgewebview2.exe") {
		t.Errorf("a folder without msedgewebview2.exe was accepted: %v", err)
	}

	// embed needs a real executable.
	boot := filepath.Join(dir, "MicrosoftEdgeWebview2Setup.exe")
	if err := os.WriteFile(boot, []byte("MZ\x90\x00"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.WebView2, m.WebView2Path = "embed", boot
	if err := addWebView2(m, newTree(), "App"); err != nil {
		t.Errorf("embed: %v", err)
	}
	if err := os.WriteFile(boot, []byte("<html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := addWebView2(m, newTree(), "App"); err == nil {
		t.Error("embed accepted a file that is not an executable")
	}
	m.WebView2Path = filepath.Join(dir, "missing.exe")
	if err := addWebView2(m, newTree(), "App"); err == nil {
		t.Error("embed accepted a missing file")
	}
}
