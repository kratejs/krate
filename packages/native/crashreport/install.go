package crashreport

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Install makes the Go runtime copy the output of an unhandled panic or fatal
// error to <dir>/pending-<pid>.log, so a later process can report it. The
// returned function ends the hook and deletes the log if nothing was written;
// call it on a clean exit. Install does nothing without a report directory.
func Install() (uninstall func()) {
	dir := Dir()
	if dir == "" {
		return func() {}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return func() {}
	}
	path := filepath.Join(dir, "pending-"+strconv.Itoa(os.Getpid())+".log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return func() {}
	}
	if err := debug.SetCrashOutput(f, debug.CrashOptions{}); err != nil {
		f.Close()
		os.Remove(path)
		return func() {}
	}
	return func() {
		_ = debug.SetCrashOutput(nil, debug.CrashOptions{})
		f.Close()
		if st, err := os.Stat(path); err == nil && st.Size() == 0 {
			os.Remove(path)
		}
	}
}

// Collect turns the crash logs of processes that are gone into reports and
// deletes the logs. Logs of live processes are left alone. It returns the
// reports it made.
func Collect() []Summary {
	mu.Lock()
	c := cfg
	dir := dirLocked()
	mu.Unlock()
	if dir == "" {
		return nil
	}
	return collectIn(dir, c, pidAlive)
}

func collectIn(dir string, c Config, alive func(pid int) bool) []Summary {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Summary
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "pending-") || !strings.HasSuffix(name, ".log") {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(name, "pending-"), ".log"))
		if err != nil || pid == os.Getpid() || alive(pid) {
			continue
		}
		path := filepath.Join(dir, name)
		st, err := os.Stat(path)
		if err != nil {
			continue
		}
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		kind, reason, stack, ok := ParseCrashLog(string(b))
		if ok {
			r := Report{Kind: kind, Reason: reason, Stack: stack, Time: st.ModTime().UTC().Format(time.RFC3339)}
			mu.Lock()
			seq++
			n := seq
			mu.Unlock()
			if id, err := writeTo(dir, c, r, n); err == nil {
				sum := Summary{ID: id, Kind: kind, Time: r.Time, Reason: reason}
				out = append(out, sum)
				notify(sum, r.Process)
			} else {
				continue // keep the log for the next try
			}
		}
		os.Remove(path)
	}
	return out
}

// Guard runs f and, if it panics, writes a "panic" report with the stack and
// panics again with the same value. Use it around the main work of a process
// whose panics would otherwise print and exit unreported (the crash log of
// Install covers those too; Guard also reports a panic recovered further up).
func Guard(process string, f func()) {
	defer func() {
		if v := recover(); v != nil {
			ReportValue(process, "panic", v)
			panic(v)
		}
	}()
	f()
}

// ReportValue writes a report for a recovered value, with the current stack.
func ReportValue(process, kind string, v any) string {
	id, _ := Write(Report{Kind: kind, Process: process, Reason: panicText(v), Stack: string(debug.Stack())})
	return id
}

var (
	subMu sync.Mutex
	subs  = map[int]func(Summary, string){}
	subN  int
)

// Subscribe calls fn for every report Collect makes from the crash log of a
// dead process, with the role of the process that crashed.
func Subscribe(fn func(s Summary, process string)) (cancel func()) {
	subMu.Lock()
	defer subMu.Unlock()
	subN++
	id := subN
	subs[id] = fn
	return func() {
		subMu.Lock()
		delete(subs, id)
		subMu.Unlock()
	}
}

func notify(s Summary, process string) {
	subMu.Lock()
	var fns []func(Summary, string)
	for _, f := range subs {
		fns = append(fns, f)
	}
	subMu.Unlock()
	for _, f := range fns {
		f(s, process)
	}
}
