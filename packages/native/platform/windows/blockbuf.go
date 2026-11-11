package windows

import (
	"bytes"
	"errors"
	"io"
	"sync"
)

// errWouldBlock is returned by TryRead when no data is buffered yet.
var errWouldBlock = errors.New("windows: no data available yet")

// blockBuf is a bounded in-memory pipe. The HTTP handler goroutine writes,
// the WebView2 side reads through the Go IStream. Write blocks while the
// buffer is full; Read blocks while it is empty. Cancel (reader went away)
// fails pending and future writes.
type blockBuf struct {
	mu       sync.Mutex
	cond     *sync.Cond
	buf      bytes.Buffer
	max      int
	closed   bool
	err      error
	canceled bool
	onCancel func()
}

func newBlockBuf(max int) *blockBuf {
	b := &blockBuf{max: max}
	b.cond = sync.NewCond(&b.mu)
	return b
}

// prime appends data without honoring the bound. It is used once, before the
// reader is attached, so a large first write cannot deadlock.
func (b *blockBuf) prime(p []byte) {
	b.mu.Lock()
	b.buf.Write(p)
	b.mu.Unlock()
}

// setOnCancel registers f to run once when the reader cancels.
func (b *blockBuf) setOnCancel(f func()) {
	b.mu.Lock()
	b.onCancel = f
	b.mu.Unlock()
}

// Write implements io.Writer.
func (b *blockBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for len(p) > 0 {
		for b.buf.Len() >= b.max && !b.canceled && !b.closed {
			b.cond.Wait()
		}
		if b.canceled || b.closed {
			return n, io.ErrClosedPipe
		}
		chunk := b.max - b.buf.Len()
		if chunk > len(p) {
			chunk = len(p)
		}
		b.buf.Write(p[:chunk])
		p = p[chunk:]
		n += chunk
		b.cond.Broadcast()
	}
	if b.canceled || b.closed {
		return n, io.ErrClosedPipe
	}
	return n, nil
}

// Read blocks until data is available, the writer closed (io.EOF or the
// writer's error) or the reader canceled (io.ErrClosedPipe).
func (b *blockBuf) Read(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for b.buf.Len() == 0 && !b.closed && !b.canceled {
		b.cond.Wait()
	}
	return b.readLocked(p)
}

// TryRead is Read that returns errWouldBlock instead of waiting.
func (b *blockBuf) TryRead(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.buf.Len() == 0 && !b.closed && !b.canceled {
		return 0, errWouldBlock
	}
	return b.readLocked(p)
}

func (b *blockBuf) readLocked(p []byte) (int, error) {
	if b.canceled {
		return 0, io.ErrClosedPipe
	}
	if b.buf.Len() > 0 && len(p) > 0 {
		n, _ := b.buf.Read(p)
		b.cond.Broadcast()
		return n, nil
	}
	if b.buf.Len() > 0 {
		return 0, nil
	}
	if b.err != nil {
		return 0, b.err
	}
	return 0, io.EOF
}

// Close marks the end of the data; buffered data is still readable.
func (b *blockBuf) Close() { b.CloseWithError(nil) }

// CloseWithError is Close with a terminal error for the reader.
func (b *blockBuf) CloseWithError(err error) {
	b.mu.Lock()
	if !b.closed {
		b.closed = true
		b.err = err
	}
	b.cond.Broadcast()
	b.mu.Unlock()
}

// Cancel is called by the reader side when it no longer wants data.
func (b *blockBuf) Cancel() {
	b.mu.Lock()
	var f func()
	if !b.canceled {
		b.canceled = true
		f = b.onCancel
		b.onCancel = nil
	}
	b.cond.Broadcast()
	b.mu.Unlock()
	if f != nil {
		f()
	}
}
