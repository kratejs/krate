package build

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// RunLogTimeFormat names run log files. It has no colons so it is a valid file
// name on Windows.
const RunLogTimeFormat = "2006-01-02T15-04-05"

var runLogPath atomic.Value // string: the file of the active run log

// currentRunLogPath is the file the active run log writes to, "" when off.
func currentRunLogPath() string {
	p, _ := runLogPath.Load().(string)
	return p
}

// StartRunLog saves this run's console output to <logs.dir>/<timestamp>.log
// when logs.enabled is set, and removes the oldest logs beyond logs.keep. It
// returns a function that stops the capture and reports the file path ("" when
// logging is off or the file cannot be created; the reason is printed).
func StartRunLog(root string, cfg *config.Config, command string) func() string {
	none := func() string { return "" }
	if cfg == nil || !cfg.Logs.Enabled {
		return none
	}
	dir, err := cfg.Logs.LogsPath(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ! logs: %v\n", err)
		return none
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "  ! logs: cannot create %s: %v\n", dir, err)
		return none
	}
	path := filepath.Join(dir, time.Now().Format(RunLogTimeFormat)+".log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ! logs: cannot create %s: %v\n", path, err)
		return none
	}
	pruneRunLogs(dir, cfg.Logs.KeepCount(), path)
	runLogPath.Store(path)
	_, _ = fmt.Fprintf(f, "%s krate %s\n", time.Now().UTC().Format(DevLogTimeFormat), command)
	stop := teeStdio(func(line []byte) {
		_, _ = f.WriteString(time.Now().UTC().Format(DevLogTimeFormat) + " " + string(line) + "\n")
	})
	return func() string {
		stop()
		_ = f.Close()
		runLogPath.Store("")
		return path
	}
}

// pruneRunLogs deletes the oldest *.log files in dir so that at most keep
// remain, never touching current. A negative keep keeps everything.
func pruneRunLogs(dir string, keep int, current string) {
	if keep < 0 {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".log") {
			names = append(names, e.Name())
		}
	}
	// Timestamped names sort oldest first.
	sort.Strings(names)
	for len(names) > keep {
		victim := filepath.Join(dir, names[0])
		names = names[1:]
		if victim == current {
			continue
		}
		_ = os.Remove(victim)
	}
}
