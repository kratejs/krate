//go:build windows

package windows

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	xw "golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// The loader finds the installed Evergreen WebView2 runtime without
// WebView2Loader.dll, the way the official loader does: read the runtime's
// EdgeUpdate client registration, load EmbeddedBrowserWebView.dll from the
// runtime folder and call its CreateWebViewEnvironmentWithOptionsInternal.
//
// The shape of that export (bool, runtime type, user data folder, options,
// handler) follows the open reimplementations of the loader (go-webview2 and
// the webview/webview project) and has not been confirmed against Microsoft
// documentation, which does not describe it. If it fails, WebView2Loader.dll
// next to the executable is tried through its documented export.

const (
	runtimeClientGUID = "{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}"
	internalExport    = "CreateWebViewEnvironmentWithOptionsInternal"
	publicExport      = "CreateCoreWebView2EnvironmentWithOptions"
	// runtimeTypeInstalled is the "installed" (Evergreen) runtime type and
	// runtimeTypeRedist the one of a Fixed Version runtime folder. The value of
	// the second is from the open reimplementations of the loader, not from
	// Microsoft documentation.
	runtimeTypeInstalled = 0
	runtimeTypeRedist    = 1

	// fixedRuntimeDirName is the folder next to the executable that holds a
	// Fixed Version runtime (native.bundle.windows.webview2 "fixed"). The
	// environment variable below overrides it; Microsoft documents it as the
	// way to point WebView2 at a browser executable folder.
	fixedRuntimeDirName = "WebView2Runtime"
	fixedRuntimeEnv     = "WEBVIEW2_BROWSER_EXECUTABLE_FOLDER"

	simulateNoRuntimeEnv = "KRATE_NATIVE_SIMULATE_NO_WEBVIEW2"
)

type runtimeInfo struct{ location, pv string }

// findRuntimes returns registered runtime installs, best first.
func findRuntimes() []runtimeInfo {
	type src struct {
		root registry.Key
		path string
	}
	base := `Microsoft\EdgeUpdate\Clients\` + runtimeClientGUID
	sources := []src{
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\` + base},
		{registry.LOCAL_MACHINE, `SOFTWARE\` + base},
		{registry.CURRENT_USER, `Software\WOW6432Node\` + base},
		{registry.CURRENT_USER, `Software\` + base},
	}
	var out []runtimeInfo
	for _, s := range sources {
		k, err := registry.OpenKey(s.root, s.path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		pv, _, _ := k.GetStringValue("pv")
		loc, _, _ := k.GetStringValue("location")
		k.Close()
		if pv == "" || pv == "0.0.0.0" {
			continue
		}
		out = append(out, runtimeInfo{location: loc, pv: pv})
	}
	return out
}

func runtimeArch() string {
	if runtime.GOARCH == "arm64" {
		return "arm64"
	}
	return "x64"
}

// fixedRuntimeDir returns the Fixed Version runtime folder to use, or "". The
// folder must hold msedgewebview2.exe.
func fixedRuntimeDir() string {
	dirs := []string{os.Getenv(fixedRuntimeEnv)}
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Join(filepath.Dir(exe), fixedRuntimeDirName))
	}
	for _, d := range dirs {
		if d == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(d, "msedgewebview2.exe")); err == nil {
			return d
		}
	}
	return ""
}

// internalLoaderCandidates lists EmbeddedBrowserWebView.dll paths to try.
func internalLoaderCandidates() []string {
	var out []string
	seen := map[string]bool{}
	add := func(list []string) {
		for _, p := range list {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	arch := runtimeArch()
	for _, r := range findRuntimes() {
		add(loaderDLLCandidates(r.location, r.pv, arch))
		// Some registrations omit or misreport location; try the default
		// per-machine install folders for the registered version.
		for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles"} {
			if pf := os.Getenv(env); pf != "" {
				add(loaderDLLCandidates(filepath.Join(pf, "Microsoft", "EdgeWebView", "Application"), r.pv, arch))
			}
		}
	}
	return out
}

// createEnvironment starts creating the WebView2 environment; handler's
// Invoke is called later from the message loop.
// opts is an optional ICoreWebView2EnvironmentOptions (nil for none).
func createEnvironment(userDataDir string, opts, handler unsafe.Pointer) error {
	// A test hook: behave as if no runtime were installed (the message box
	// test runs the app with it).
	if os.Getenv(simulateNoRuntimeEnv) != "" {
		return &runtimeError{missing: true}
	}
	var tried []string

	// A bundled Fixed Version runtime wins over an installed one: the app was
	// tested with it.
	if fixed := fixedRuntimeDir(); fixed != "" {
		_ = os.Setenv(fixedRuntimeEnv, fixed)
		for _, dll := range loaderDLLCandidates(fixed, "", runtimeArch()) {
			if _, err := os.Stat(dll); err != nil {
				continue
			}
			for _, typ := range []uintptr{runtimeTypeRedist, runtimeTypeInstalled} {
				hr, err := callInternal(dll, typ, userDataDir, opts, handler)
				if err == nil && !failed(hr) {
					return nil
				}
				if err == nil {
					err = fmt.Errorf("HRESULT 0x%08x", uint32(hr))
				}
				tried = append(tried, fmt.Sprintf("%s (type %d): %v", dll, typ, err))
			}
		}
	}

	for _, dll := range internalLoaderCandidates() {
		if _, err := os.Stat(dll); err != nil {
			continue
		}
		hr, err := callInternal(dll, runtimeTypeInstalled, userDataDir, opts, handler)
		if err == nil && !failed(hr) {
			return nil
		}
		if err == nil {
			err = fmt.Errorf("HRESULT 0x%08x", uint32(hr))
		}
		tried = append(tried, fmt.Sprintf("%s: %v", dll, err))
	}

	if exe, err := os.Executable(); err == nil {
		dll := filepath.Join(filepath.Dir(exe), "WebView2Loader.dll")
		if _, err := os.Stat(dll); err == nil {
			hr, err := callPublic(dll, fixedRuntimeDir(), userDataDir, opts, handler)
			if err == nil && !failed(hr) {
				return nil
			}
			if err == nil {
				err = fmt.Errorf("HRESULT 0x%08x", uint32(hr))
			}
			tried = append(tried, fmt.Sprintf("%s: %v", dll, err))
		}
	}

	if len(tried) > 0 {
		return &runtimeError{detail: " [" + strings.Join(tried, "; ") + "]"}
	}
	return &runtimeError{missing: true}
}

func callInternal(dll string, runtimeType uintptr, userDataDir string, opts, handler unsafe.Pointer) (uintptr, error) {
	h, err := xw.LoadLibraryEx(dll, 0, xw.LOAD_WITH_ALTERED_SEARCH_PATH)
	if err != nil {
		return 0, err
	}
	proc, err := xw.GetProcAddress(h, internalExport)
	if err != nil {
		return 0, err
	}
	r, _, _ := syscall.SyscallN(proc,
		1,           // checkRunningInstance
		runtimeType, // runtime type
		uintptr(unsafe.Pointer(wstr(userDataDir))),
		uintptr(opts), // environment options (0: none)
		uintptr(handler))
	return r, nil
}

func callPublic(dll, browserDir, userDataDir string, opts, handler unsafe.Pointer) (uintptr, error) {
	h, err := xw.LoadLibraryEx(dll, 0, xw.LOAD_WITH_ALTERED_SEARCH_PATH)
	if err != nil {
		return 0, err
	}
	proc, err := xw.GetProcAddress(h, publicExport)
	if err != nil {
		return 0, err
	}
	var browser uintptr // 0: the installed runtime
	var keep *uint16
	if browserDir != "" {
		keep = wstr(browserDir)
		browser = uintptr(unsafe.Pointer(keep))
	}
	r, _, _ := syscall.SyscallN(proc,
		browser,
		uintptr(unsafe.Pointer(wstr(userDataDir))),
		uintptr(opts), // options (0: none)
		uintptr(handler))
	runtime.KeepAlive(keep)
	return r, nil
}
