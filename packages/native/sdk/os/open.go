package sdkos

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/native/krate"
)

var (
	schemeMu sync.RWMutex
	schemes  = map[string]bool{"http": true, "https": true, "mailto": true}
)

// AllowScheme lets OpenExternal open URLs of another scheme (for example
// "tel" or an app's own custom scheme). It affects UI calls and Go calls
// alike. The defaults are http, https and mailto.
func AllowScheme(scheme string) {
	schemeMu.Lock()
	schemes[strings.ToLower(scheme)] = true
	schemeMu.Unlock()
}

// checkURL validates raw for OpenExternal.
func checkURL(raw string) error {
	if raw == "" || len(raw) > 8192 {
		return krate.Errorf("bad_args", "openExternal: the URL is empty or too long")
	}
	for _, r := range raw {
		if r < 0x20 || r == 0x7f || r == ' ' {
			return krate.Errorf("bad_args", "openExternal: the URL contains whitespace or control characters")
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return krate.Errorf("bad_args", "openExternal: %w", err)
	}
	s := strings.ToLower(u.Scheme)
	schemeMu.RLock()
	ok := schemes[s]
	schemeMu.RUnlock()
	if !ok {
		return krate.Errorf("bad_args", "openExternal: scheme %q is not allowed (allowed: http, https, mailto)", u.Scheme)
	}
	if (s == "http" || s == "https") && u.Host == "" {
		return krate.Errorf("bad_args", "openExternal: %q has no host", raw)
	}
	return nil
}

// openFunc starts the OS handler; tests replace it.
var openFunc = startHandler

// OpenExternal opens raw in the OS default handler: the browser for http and
// https, the mail client for mailto. Other schemes fail unless allowed with
// AllowScheme. It returns once the handler has started.
func OpenExternal(ctx context.Context, raw string) error {
	if err := checkURL(raw); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return openFunc(raw)
}

func startHandler(raw string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", raw)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", raw)
	default:
		cmd = exec.Command("xdg-open", raw)
	}
	if cmd.Err != nil {
		return fmt.Errorf("sdkos: no URL handler (%s): %w", cmd.Args[0], krate.ErrUnsupported)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("sdkos: starting %s: %w", cmd.Args[0], err)
	}
	go func() { _ = cmd.Wait() }() // reap the child
	return nil
}
