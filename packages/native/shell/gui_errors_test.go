package shell

import (
	"fmt"
	"os"
	"sync"
)

// guiLog keeps what the shell logs, so a test can check what reached the
// terminal of `krate native dev`.
var guiLog struct {
	mu    sync.Mutex
	lines []string
}

func guiLogf(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	guiLog.mu.Lock()
	guiLog.lines = append(guiLog.lines, line)
	guiLog.mu.Unlock()
	fmt.Fprintf(os.Stderr, "krate-shell: %s\n", line)
}
