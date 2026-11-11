//go:build windows

package windows

import (
	"errors"
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
	// runtimeTypeInstalled is the "installed" (Evergreen) runtime type.
	runtimeTypeInstalled = 0

	bootstrapperURL = "https://go.microsoft.com/fwlink/p/?LinkId=2124703"
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
func createEnvironment(userDataDir string, handler unsafe.Pointer) error {
	var tried []string

	for _, dll := range internalLoaderCandidates() {
		if _, err := os.Stat(dll); err != nil {
			continue
		}
		hr, err := callInternal(dll, userDataDir, handler)
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
			hr, err := callPublic(dll, userDataDir, handler)
			if err == nil && !failed(hr) {
				return nil
			}
			if err == nil {
				err = fmt.Errorf("HRESULT 0x%08x", uint32(hr))
			}
			tried = append(tried, fmt.Sprintf("%s: %v", dll, err))
		}
	}

	msg := "the WebView2 Runtime is not installed (or could not be started); " +
		"install the Evergreen Runtime with the bootstrapper from " + bootstrapperURL +
		", or ship WebView2Loader.dll next to the executable"
	if len(tried) > 0 {
		msg += " [" + strings.Join(tried, "; ") + "]"
	}
	return errors.New(msg)
}

func callInternal(dll, userDataDir string, handler unsafe.Pointer) (uintptr, error) {
	h, err := xw.LoadLibraryEx(dll, 0, xw.LOAD_WITH_ALTERED_SEARCH_PATH)
	if err != nil {
		return 0, err
	}
	proc, err := xw.GetProcAddress(h, internalExport)
	if err != nil {
		return 0, err
	}
	r, _, _ := syscall.SyscallN(proc,
		1,                    // checkRunningInstance
		runtimeTypeInstalled, // runtime type
		uintptr(unsafe.Pointer(wstr(userDataDir))),
		0, // environment options: none
		uintptr(handler))
	return r, nil
}

func callPublic(dll, userDataDir string, handler unsafe.Pointer) (uintptr, error) {
	h, err := xw.LoadLibraryEx(dll, 0, xw.LOAD_WITH_ALTERED_SEARCH_PATH)
	if err != nil {
		return 0, err
	}
	proc, err := xw.GetProcAddress(h, publicExport)
	if err != nil {
		return 0, err
	}
	r, _, _ := syscall.SyscallN(proc,
		0, // browser executable folder: installed runtime
		uintptr(unsafe.Pointer(wstr(userDataDir))),
		0, // options
		uintptr(handler))
	return r, nil
}
