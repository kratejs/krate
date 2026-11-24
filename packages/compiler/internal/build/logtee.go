package build

import (
	"bytes"
	"os"
	"sync"
)

// teeLine is one console line handed to the sinks of a stdio tee. src names
// the sidecar that printed it ("" for the krate process itself).
type teeLine struct {
	text   []byte
	stderr bool
	src    string
}

// stdioTee is one active teeStdio: the streams it replaced and its sinks.
type stdioTee struct {
	mu      sync.Mutex
	origOut *os.File
	origErr *os.File
	sinks   []func(teeLine)
}

// deliver hands one complete line to every sink, one sink at a time.
func (t *stdioTee) deliver(l teeLine) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, sink := range t.sinks {
		sink(l)
	}
}

// activeTees lists the running tees, innermost (the first started, whose
// original streams are the real terminal) first.
var activeTees struct {
	sync.Mutex
	list []*stdioTee
}

// teeStdio mirrors os.Stdout and os.Stderr to their original files and hands
// every complete line (ANSI codes and the trailing CR removed) to each sink.
// Sinks run one at a time. Each stream keeps its own unfinished line because
// stdout and stderr interleave at any point. The returned function restores the
// streams and waits for the pipes to drain.
func teeStdio(sinks ...func(line []byte)) func() {
	return teeStdioLines(func(l teeLine) {
		for _, sink := range sinks {
			sink(l.text)
		}
	})
}

// teeStdioLines is teeStdio with sinks that also learn the stream and the
// source of each line.
func teeStdioLines(sinks ...func(teeLine)) func() {
	t := &stdioTee{origOut: os.Stdout, origErr: os.Stderr, sinks: sinks}
	write := func(partial *[]byte, p []byte, stderr bool) {
		t.mu.Lock()
		defer t.mu.Unlock()
		data := append(*partial, p...)
		*partial = nil
		for {
			i := bytes.IndexByte(data, '\n')
			if i < 0 {
				break
			}
			line := devLogANSI.ReplaceAll(bytes.TrimRight(data[:i], "\r"), nil)
			for _, sink := range t.sinks {
				sink(teeLine{text: line, stderr: stderr})
			}
			data = data[i+1:]
		}
		*partial = append([]byte(nil), data...)
	}

	origOut, origErr := os.Stdout, os.Stderr
	var restores []func()
	var wg sync.WaitGroup
	tee := func(orig *os.File, stderr bool, set func(*os.File)) {
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
					write(&partial, buf[:n], stderr)
				}
				if err != nil {
					if len(partial) > 0 {
						write(&partial, []byte("\n"), stderr)
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
	tee(origOut, false, func(f *os.File) { os.Stdout = f })
	tee(origErr, true, func(f *os.File) { os.Stderr = f })
	SetOutput(os.Stdout)
	activeTees.Lock()
	activeTees.list = append(activeTees.list, t)
	activeTees.Unlock()
	return func() {
		activeTees.Lock()
		for i, x := range activeTees.list {
			if x == t {
				activeTees.list = append(activeTees.list[:i:i], activeTees.list[i+1:]...)
				break
			}
		}
		activeTees.Unlock()
		for _, r := range restores {
			r()
		}
		wg.Wait()
		SetOutput(origOut)
	}
}

// writeConsoleLine prints one line a sidecar wrote (raw ends with its
// newline, when it has one) exactly as the pipe of the tees would have: the
// bytes go to the real terminal and every tee's sinks see the line, now with
// its source attached. Without a tee it goes to os.Stdout or os.Stderr.
func writeConsoleLine(src string, stderr bool, raw []byte) {
	activeTees.Lock()
	tees := append([]*stdioTee(nil), activeTees.list...)
	activeTees.Unlock()
	text := devLogANSI.ReplaceAll(bytes.TrimRight(bytes.TrimRight(raw, "\n"), "\r"), nil)
	if len(tees) == 0 {
		out := os.Stdout
		if stderr {
			out = os.Stderr
		}
		_, _ = out.Write(raw)
	} else {
		out := tees[0].origOut
		if stderr {
			out = tees[0].origErr
		}
		_, _ = out.Write(raw)
		for _, t := range tees {
			t.deliver(teeLine{text: text, stderr: stderr, src: src})
		}
	}
	recordDevLog(src, stderr, string(text))
}
