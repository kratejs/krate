package build

import (
	"bufio"
	"os"
	"strings"
	"sync"
	"sync/atomic"
)

// devShortcuts are the keys the dev server listens for on stdin. The terminal
// is read line by line, so each is followed by Enter, as in other dev servers.
type devShortcuts struct {
	hub *DevHub
	url string
}

type shortcutAction int

const (
	shortcutNone shortcutAction = iota
	shortcutRestart
	shortcutOpen
	shortcutClear
	shortcutHelp
)

// parseShortcut maps a line of input to an action.
func parseShortcut(line string) shortcutAction {
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "r":
		return shortcutRestart
	case "o":
		return shortcutOpen
	case "c":
		return shortcutClear
	case "h", "?":
		return shortcutHelp
	}
	return shortcutNone
}

const shortcutHint = "press r + enter to restart, o to open the browser, c to clear, h for help"

// browserOpened records that this process already opened the browser.
var browserOpened atomic.Bool

var (
	shortcutMu     sync.Mutex
	shortcutActive *devShortcuts
	shortcutOnce   sync.Once
	stdinIsTTY     = isTTY(os.Stdin)
)

// registerDevShortcuts makes the running dev server the target of the stdin
// shortcuts and returns the function that detaches it. One reader goroutine
// serves every server, because a restart starts a new one while the old reader
// is still blocked on stdin. It does nothing unless enabled (`krate dev
// --repl`) and stdin is a terminal; without the flag it only prints a hint.
func registerDevShortcuts(hub *DevHub, url string, enabled bool) func() {
	if !stdinIsTTY {
		return func() {}
	}
	if !enabled {
		printf("  %sadd --repl for terminal shortcuts (restart, open, clear)%s\n", cGray, cReset)
		return func() {}
	}
	me := &devShortcuts{hub: hub, url: url}
	shortcutMu.Lock()
	shortcutActive = me
	shortcutMu.Unlock()
	shortcutOnce.Do(func() { go readShortcuts(bufio.NewReader(os.Stdin)) })
	printf("  %s%s%s\n", cGray, shortcutHint, cReset)
	return func() {
		shortcutMu.Lock()
		if shortcutActive == me {
			shortcutActive = nil
		}
		shortcutMu.Unlock()
	}
}

func readShortcuts(in *bufio.Reader) {
	for {
		line, err := in.ReadString('\n')
		if action := parseShortcut(line); action != shortcutNone {
			shortcutMu.Lock()
			s := shortcutActive
			shortcutMu.Unlock()
			if s != nil {
				s.run(action)
			}
		}
		if err != nil {
			return
		}
	}
}

func (s *devShortcuts) run(a shortcutAction) {
	switch a {
	case shortcutRestart:
		printf("  %srestarting...%s\n", cYellow, cReset)
		s.hub.RequestRestart()
	case shortcutOpen:
		openBrowser(s.url)
	case shortcutClear:
		// Clear the screen and the scrollback, then home the cursor.
		printf("\x1b[2J\x1b[3J\x1b[H")
	case shortcutHelp:
		printf("  %s%s%s\n", cGray, shortcutHint, cReset)
	}
}
