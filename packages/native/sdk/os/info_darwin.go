//go:build darwin

package sdkos

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

func run(name string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func osVersion() string { return run("/usr/bin/sw_vers", "-productVersion") }

func systemLocale() string {
	if l := localeFromEnv(os.Getenv); l != "" {
		return l
	}
	// AppleLocale looks like en_US or en_US@rg=gbzzzz.
	l := run("/usr/bin/defaults", "read", "-g", "AppleLocale")
	if i := strings.IndexAny(l, "@."); i >= 0 {
		l = l[:i]
	}
	return strings.ReplaceAll(l, "_", "-")
}
