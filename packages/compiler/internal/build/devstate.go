package build

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

const maxClientErrors = 100

// ClientErrorEntry is a browser error with the time the dev server received it.
type ClientErrorEntry struct {
	ClientError
	Time time.Time `json:"time"`
}

// DevSnapshot is the machine-readable dev server state served at
// /__krate/state. The MCP server reads it to report build, browser and runtime
// errors from the live `krate dev` process.
type DevSnapshot struct {
	PID          int                `json:"pid"`
	BuildOK      bool               `json:"buildOk"`
	Built        bool               `json:"built"`
	Diagnostics  []diag.Diagnostic  `json:"diagnostics"`
	Routes       []string           `json:"routes,omitempty"`
	ClientErrors []ClientErrorEntry `json:"clientErrors"`
	LastEventAt  *time.Time         `json:"lastEventAt,omitempty"`
	Rebuilds     int                `json:"rebuilds"`
}

// Snapshot returns the current dev state: the latest build outcome plus the
// most recent browser errors.
func (h *DevHub) Snapshot() DevSnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	snap := DevSnapshot{
		PID:          os.Getpid(),
		BuildOK:      true,
		Built:        true, // the server only answers after the initial build attempt
		Diagnostics:  []diag.Diagnostic{},
		ClientErrors: append([]ClientErrorEntry{}, h.clientErrs...),
		Rebuilds:     h.rebuilds,
	}
	if h.last != nil {
		snap.BuildOK = h.last.BuildOK
		snap.Routes = h.last.Routes
		if len(h.last.Diagnostics) > 0 {
			snap.Diagnostics = append(snap.Diagnostics, h.last.Diagnostics...)
		}
		t := h.lastEventAt
		snap.LastEventAt = &t
	}
	return snap
}

// DevServerRecord describes a running `krate dev` process. It is written to
// .krate/dev-server.json so tools (the MCP server) can find the live server.
type DevServerRecord struct {
	PID       int       `json:"pid"`
	Host      string    `json:"host"`
	Port      int       `json:"port"`
	URL       string    `json:"url"`
	StartedAt time.Time `json:"startedAt"`
	LogFile   string    `json:"logFile,omitempty"`
}

// DevServerRecordPath is the discovery file for a project's dev server.
func DevServerRecordPath(root string) string {
	return filepath.Join(root, ".krate", "dev-server.json")
}

// DevLogPath is the file `krate dev` mirrors its console output into.
func DevLogPath(root string) string {
	return filepath.Join(root, ".krate", "dev.log")
}

// ReadDevServerRecord loads the discovery file. ok is false when it is absent
// or unreadable.
func ReadDevServerRecord(root string) (DevServerRecord, bool) {
	data, err := os.ReadFile(DevServerRecordPath(root))
	if err != nil {
		return DevServerRecord{}, false
	}
	var rec DevServerRecord
	if json.Unmarshal(data, &rec) != nil || rec.Port == 0 {
		return DevServerRecord{}, false
	}
	return rec, true
}

// writeDevServerRecord records the running dev server and returns a function
// that removes the record again (only if it still names this process).
func writeDevServerRecord(root string, rec DevServerRecord) func() {
	path := DevServerRecordPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return func() {}
	}
	rec.PID = os.Getpid()
	if _, err := os.Stat(DevLogPath(root)); err == nil {
		rec.LogFile = filepath.ToSlash(filepath.Join(".krate", "dev.log"))
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return func() {}
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return func() {}
	}
	return func() {
		if cur, ok := ReadDevServerRecord(root); ok && cur.PID == rec.PID {
			_ = os.Remove(path)
		}
	}
}

var devLogANSI = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]")

// DevLogTimeFormat prefixes every line of .krate/dev.log.
const DevLogTimeFormat = "2006-01-02T15:04:05Z"

const devLogMax = 4 << 20

// TeeDevLog mirrors os.Stdout and os.Stderr into .krate/dev.log (ANSI codes
// removed, truncated when it grows past a few MB) so an agent can read the
// server output. The returned function restores the streams. It is a no-op
// when the log file cannot be created.
func TeeDevLog(root string) func() {
	path := DevLogPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return func() {}
	}
	f, err := os.Create(path)
	if err != nil {
		return func() {}
	}
	var mu sync.Mutex
	written := 0
	emit := func(line []byte) {
		if written > devLogMax {
			_ = f.Truncate(0)
			_, _ = f.Seek(0, io.SeekStart)
			written = 0
			_, _ = f.WriteString(time.Now().UTC().Format(DevLogTimeFormat) + " [log truncated]\n")
		}
		n, _ := f.WriteString(time.Now().UTC().Format(DevLogTimeFormat) + " " + string(line) + "\n")
		written += n
	}
	// write splits one stream into lines and stamps each with its time, so
	// readers can ask for "errors since the last rebuild". Each stream keeps its
	// own unfinished line: stdout and stderr interleave at any point.
	write := func(partial *[]byte, p []byte) {
		mu.Lock()
		defer mu.Unlock()
		data := append(*partial, p...)
		*partial = nil
		for {
			i := bytes.IndexByte(data, '\n')
			if i < 0 {
				break
			}
			emit(devLogANSI.ReplaceAll(bytes.TrimRight(data[:i], "\r"), nil))
			data = data[i+1:]
		}
		*partial = append([]byte(nil), data...)
	}

	origOut, origErr := os.Stdout, os.Stderr
	var restores []func()
	var wg sync.WaitGroup
	tee := func(orig *os.File, set func(*os.File)) {
		r, w, err := os.Pipe()
		if err != nil {
			return
		}
		set(w)
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 8192)
			var partial []byte
			for {
				n, err := r.Read(buf)
				if n > 0 {
					_, _ = orig.Write(buf[:n])
					write(&partial, buf[:n])
				}
				if err != nil {
					if len(partial) > 0 {
						write(&partial, []byte("\n"))
					}
					return
				}
			}
		}()
		restores = append(restores, func() {
			set(orig)
			_ = w.Close()
		})
	}
	tee(origOut, func(f *os.File) { os.Stdout = f })
	tee(origErr, func(f *os.File) { os.Stderr = f })
	SetOutput(os.Stdout)
	return func() {
		for _, r := range restores {
			r()
		}
		wg.Wait()
		SetOutput(origOut)
		_ = f.Close()
	}
}
