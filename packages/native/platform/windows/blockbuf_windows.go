package windows

// setOnCancel registers f to run once when the reader cancels.
func (b *blockBuf) setOnCancel(f func()) {
	b.mu.Lock()
	b.onCancel = f
	b.mu.Unlock()
}
