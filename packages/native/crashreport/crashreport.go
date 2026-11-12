// Package crashreport writes crash reports to disk and reads them back. It has
// no dependencies on the rest of the SDK so that the shell, the Go host child
// and the SDK can all use it.
//
// A report is one JSON file, <dir>/crash-<time>-<n>.json, holding the app and
// OS versions, the process role and the Go stack. Go crashes that end the
// process cannot run code, so Install asks the runtime to copy its crash output
// to <dir>/pending-<pid>.log (runtime/debug.SetCrashOutput); the next process
// to call List or Collect turns the log of a dead process into a report.
// Reports stay on disk until the app deletes them. Nothing is sent anywhere.
package crashreport

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DirEnv names the environment variable that gives child processes the report
// directory of the shell.
const DirEnv = "KRATE_CRASH_DIR"

// maxReports is how many reports are kept; older ones are deleted on write.
const maxReports = 50

// maxStack caps the stack stored in a report.
const maxStack = 64 << 10

// Report is one crash report.
type Report struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"` // "panic", "fatal" or "webview"
	Time         string `json:"time"` // RFC 3339
	Reason       string `json:"reason"`
	AppID        string `json:"appId"`
	AppVersion   string `json:"appVersion"`
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	OSVersion    string `json:"osVersion"`
	GoVersion    string `json:"goVersion"`
	ShellVersion string `json:"shellVersion"`
	Process      string `json:"process"` // "shell", "host" or "webview"
	Window       string `json:"window,omitempty"`
	View         string `json:"view,omitempty"`
	Stack        string `json:"stack,omitempty"`
}

// Summary is a Report without the bulk, for listings.
type Summary struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Time   string `json:"time"`
	Reason string `json:"reason"`
}

// Config describes the running app.
type Config struct {
	// Dir is where reports live; it is created on first write. Empty means
	// the KRATE_CRASH_DIR environment variable.
	Dir          string
	AppID        string
	AppVersion   string
	OSVersion    string
	ShellVersion string
	// Process is this process's role: "shell" or "host".
	Process string
}

var (
	mu  sync.Mutex
	cfg Config
	seq int
)

// Configure sets the app description and report directory for this process.
func Configure(c Config) {
	mu.Lock()
	defer mu.Unlock()
	cfg = c
}

// Dir returns the report directory, "" if none is configured.
func Dir() string {
	mu.Lock()
	defer mu.Unlock()
	return dirLocked()
}

func dirLocked() string {
	if cfg.Dir != "" {
		return cfg.Dir
	}
	return os.Getenv(DirEnv)
}

var idRe = regexp.MustCompile(`^crash-[0-9a-z-]+$`)

// ErrNoDir is returned when no report directory is configured.
var ErrNoDir = errors.New("crashreport: no report directory is configured")

// ErrNotFound is returned for an id with no report.
var ErrNotFound = errors.New("crashreport: no such report")

func (c Config) fill(r *Report) {
	r.AppID, r.AppVersion, r.OSVersion, r.ShellVersion = c.AppID, c.AppVersion, c.OSVersion, c.ShellVersion
	if r.Process == "" {
		r.Process = c.Process
	}
	r.OS, r.Arch, r.GoVersion = runtime.GOOS, runtime.GOARCH, runtime.Version()
	if r.ShellVersion == "" {
		if bi, ok := debug.ReadBuildInfo(); ok {
			r.ShellVersion = bi.Main.Version
		}
	}
}

// Write stores r (filling in the versions, id and time) and returns its id.
func Write(r Report) (string, error) {
	mu.Lock()
	c := cfg
	dir := dirLocked()
	seq++
	n := seq
	mu.Unlock()
	if dir == "" {
		return "", ErrNoDir
	}
	return writeTo(dir, c, r, n)
}

func writeTo(dir string, c Config, r Report, n int) (string, error) {
	c.fill(&r)
	t := time.Now()
	if r.Time != "" {
		if pt, err := time.Parse(time.RFC3339, r.Time); err == nil {
			t = pt
		}
	}
	r.Time = t.UTC().Format(time.RFC3339)
	r.ID = "crash-" + strconv.FormatInt(t.UnixMilli(), 36) + "-" + strconv.Itoa(os.Getpid()) + "-" + strconv.Itoa(n)
	if len(r.Stack) > maxStack {
		r.Stack = r.Stack[:maxStack] + "\n... (truncated)\n"
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	tmp := filepath.Join(dir, r.ID+".tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, filepath.Join(dir, r.ID+".json")); err != nil {
		return "", err
	}
	prune(dir)
	return r.ID, nil
}

// prune deletes the oldest reports beyond maxReports.
func prune(dir string) {
	l := listDir(dir)
	for i := maxReports; i < len(l); i++ {
		_ = os.Remove(filepath.Join(dir, l[i].ID+".json"))
	}
}

func listDir(dir string) []Summary {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Summary
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".json") || !idRe.MatchString(strings.TrimSuffix(name, ".json")) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var r Report
		if json.Unmarshal(b, &r) != nil {
			continue
		}
		out = append(out, Summary{ID: strings.TrimSuffix(name, ".json"), Kind: r.Kind, Time: r.Time, Reason: r.Reason})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Time != out[j].Time {
			return out[i].Time > out[j].Time
		}
		return out[i].ID > out[j].ID
	})
	return out
}

// List returns the reports, newest first. It first turns the crash logs of
// dead processes into reports (see Collect).
func List() ([]Summary, error) {
	dir := Dir()
	if dir == "" {
		return nil, ErrNoDir
	}
	Collect()
	return listDir(dir), nil
}

// Read returns the report with this id.
func Read(id string) (Report, error) {
	dir := Dir()
	if dir == "" {
		return Report{}, ErrNoDir
	}
	if !idRe.MatchString(id) {
		return Report{}, fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	b, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if errors.Is(err, fs.ErrNotExist) {
		return Report{}, fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	if err != nil {
		return Report{}, err
	}
	var r Report
	if err := json.Unmarshal(b, &r); err != nil {
		return Report{}, fmt.Errorf("crashreport: %s is damaged: %w", id, err)
	}
	return r, nil
}

// Delete removes a report; a missing one is not an error.
func Delete(id string) error {
	dir := Dir()
	if dir == "" {
		return ErrNoDir
	}
	if !idRe.MatchString(id) {
		return fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	if err := os.Remove(filepath.Join(dir, id+".json")); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// ParseCrashLog extracts the kind, reason and stack from the text the Go
// runtime printed when it crashed. ok is false when the text is empty.
func ParseCrashLog(text string) (kind, reason, stack string, ok bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", "", "", false
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "panic: "):
			return "panic", strings.TrimPrefix(line, "panic: "), text, true
		case strings.HasPrefix(line, "fatal error: "):
			return "fatal", strings.TrimPrefix(line, "fatal error: "), text, true
		}
	}
	return "fatal", firstLine(text), text, true
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(l)
}

func panicText(v any) string {
	switch x := v.(type) {
	case error:
		return x.Error()
	case string:
		return x
	case fmt.Stringer:
		return x.String()
	}
	return fmt.Sprint(v)
}
