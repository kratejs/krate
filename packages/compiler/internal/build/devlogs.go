package build

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// devLogCap is how many console lines the overlay's Logs panel can page
// through.
const devLogCap = 500

// Log sources of DevLogLine.Src.
const (
	LogSrcKrate      = "krate"
	LogSrcSSR        = "ssr"
	LogSrcAPI        = "api"
	LogSrcGo         = "go"
	LogSrcMiddleware = "middleware"
	LogSrcBrowser    = "browser"
	LogSrcServer     = "server"
)

// DevLogLine is one console line of the dev server.
type DevLogLine struct {
	Seq  int64  `json:"seq"`
	T    int64  `json:"t"` // unix ms
	Src  string `json:"src"`
	Lvl  string `json:"lvl"`
	Text string `json:"text"`
}

// devLogRing keeps the latest console lines with increasing sequence numbers.
var devLogRing struct {
	sync.Mutex
	seq   int64
	lines []DevLogLine
}

// devLogsOn counts the running dev servers; sidecar output is attributed to
// its source only while one runs.
var devLogsOn atomic.Int32

// devLogLineMax caps a stored line; the terminal and dev.log keep it whole.
const devLogLineMax = 4 << 10

// recordDevLog stores one console line. src "" is the krate process itself,
// which is attributed by its content ("[browser]" lines).
func recordDevLog(src string, stderr bool, text string) {
	if src == "" {
		src = LogSrcKrate
		if strings.HasPrefix(strings.TrimSpace(text), "[browser]") {
			src = LogSrcBrowser
		}
	}
	if strings.TrimSpace(text) == "" {
		return
	}
	if len(text) > devLogLineMax {
		text = text[:devLogLineMax] + "..."
	}
	devLogRing.Lock()
	defer devLogRing.Unlock()
	devLogRing.seq++
	devLogRing.lines = append(devLogRing.lines, DevLogLine{
		Seq:  devLogRing.seq,
		T:    time.Now().UnixMilli(),
		Src:  src,
		Lvl:  logLevel(stderr, text),
		Text: text,
	})
	if over := len(devLogRing.lines) - devLogCap; over > 0 {
		devLogRing.lines = append([]DevLogLine(nil), devLogRing.lines[over:]...)
	}
}

// DevLogsSince returns the stored lines with a sequence number above since,
// oldest first, and the latest sequence number.
func DevLogsSince(since int64) (int64, []DevLogLine) {
	devLogRing.Lock()
	defer devLogRing.Unlock()
	out := []DevLogLine{}
	for _, l := range devLogRing.lines {
		if l.Seq > since {
			out = append(out, l)
		}
	}
	return devLogRing.seq, out
}

var (
	logErrorRe = regexp.MustCompile(`(?i:\b(error|errors|failed|failure|panic|fatal|exception|uncaught|unhandled)\b)|\b[A-Z][A-Za-z]*Error\b`)
	logWarnRe  = regexp.MustCompile(`(?i:\b(warn|warns|warning|warnings)\b|Warning\b|\bdeprecat)`)
)

// logLevel classifies a line: krate's own markers ("x " errors, "! "
// warnings) first, then warning and error words, then the progress markers
// ("+ ", "* ", "> ") as information; anything else on stderr is
// an error, on stdout information.
func logLevel(stderr bool, text string) string {
	t := strings.TrimSpace(text)
	switch {
	case strings.HasPrefix(t, "! "):
		return "warn"
	case strings.HasPrefix(t, "x "):
		return "error"
	case logWarnRe.MatchString(t):
		return "warn"
	case logErrorRe.MatchString(t):
		return "error"
	case strings.HasPrefix(t, "+ "), strings.HasPrefix(t, "* "), strings.HasPrefix(t, "> "):
		// krate's progress markers ("+ SSR renderer restarted") are
		// information, whichever stream they went to.
		return "info"
	case stderr:
		return "error"
	}
	return "info"
}

// printAttributed prints text (whole lines) to stdout or stderr as the krate
// process would, but while a dev server runs the overlay's Logs panel gets
// each line with src instead of "krate": krate prints some lines on behalf of
// a sidecar (API route failures of the embedded runtime). The terminal and
// .krate/dev.log see the same bytes either way.
func printAttributed(src string, stderr bool, text string) {
	if devLogsOn.Load() <= 0 {
		out := os.Stdout
		if stderr {
			out = os.Stderr
		}
		_, _ = io.WriteString(out, text)
		return
	}
	for text != "" {
		i := strings.IndexByte(text, '\n')
		if i < 0 {
			writeConsoleLine(src, stderr, []byte(text+"\n"))
			return
		}
		writeConsoleLine(src, stderr, []byte(text[:i+1]))
		text = text[i+1:]
	}
}

// attachSidecarOutput points cmd's stdout and stderr at the console. While a
// dev server runs the output goes through pipes that tag each line with src
// for the overlay's Logs panel; the terminal, .krate/dev.log and the run log
// get the same bytes as before. Call the returned function right after
// cmd.Start, whether it succeeded or not.
func attachSidecarOutput(cmd *exec.Cmd, src string) func() {
	if devLogsOn.Load() <= 0 {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return func() {}
	}
	var closers []io.Closer
	pipe := func(stderr bool) *os.File {
		r, w, err := os.Pipe()
		if err != nil {
			if stderr {
				return os.Stderr
			}
			return os.Stdout
		}
		closers = append(closers, w)
		go copyConsoleLines(r, src, stderr)
		return w
	}
	cmd.Stdout = pipe(false)
	cmd.Stderr = pipe(true)
	return func() {
		// The child holds its own copies; closing ours lets the readers see
		// EOF when it exits.
		for _, c := range closers {
			_ = c.Close()
		}
	}
}

// copyConsoleLines forwards a sidecar stream to the console line by line.
func copyConsoleLines(r *os.File, src string, stderr bool) {
	defer r.Close()
	buf := make([]byte, 8192)
	var partial []byte
	for {
		n, err := r.Read(buf)
		if n > 0 {
			data := append(partial, buf[:n]...)
			partial = nil
			for {
				i := bytes.IndexByte(data, '\n')
				if i < 0 {
					break
				}
				writeConsoleLine(src, stderr, data[:i+1])
				data = data[i+1:]
			}
			if len(data) > 0 {
				partial = append([]byte(nil), data...)
			}
		}
		if err != nil {
			if len(partial) > 0 {
				writeConsoleLine(src, stderr, partial)
			}
			return
		}
	}
}
