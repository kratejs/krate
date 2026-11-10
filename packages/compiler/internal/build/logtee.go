package build

import (
	"bytes"
	"os"
	"sync"
)

// teeStdio mirrors os.Stdout and os.Stderr to their original files and hands
// every complete line (ANSI codes and the trailing CR removed) to each sink.
// Sinks run one at a time. Each stream keeps its own unfinished line because
// stdout and stderr interleave at any point. The returned function restores the
// streams and waits for the pipes to drain.
func teeStdio(sinks ...func(line []byte)) func() {
	var mu sync.Mutex
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
			line := devLogANSI.ReplaceAll(bytes.TrimRight(data[:i], "\r"), nil)
			for _, sink := range sinks {
				sink(line)
			}
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
	}
}
