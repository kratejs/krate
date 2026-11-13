//go:build darwin

package darwin

import (
	"fmt"
	"strings"
)

// NSEventModifierFlag values (NSEvent.h) as used for menu key equivalents.
const (
	keyModShift   uint = 1 << 17
	keyModControl uint = 1 << 18
	keyModOption  uint = 1 << 19
	keyModCommand uint = 1 << 20
)

// menuFunctionKeys are the key names that have no printable character; the
// value is the private-use character AppKit expects (NSEvent.h).
var menuFunctionKeys = map[string]rune{
	"up": 0xF700, "down": 0xF701, "left": 0xF702, "right": 0xF703,
	"insert": 0xF727, "delete": 0xF728, "home": 0xF729, "end": 0xF72B,
	"pageup": 0xF72C, "pagedown": 0xF72D,
}

var menuControlKeys = map[string]string{
	"space": " ", "enter": "\r", "return": "\r", "tab": "\t",
	"esc": "\x1b", "escape": "\x1b", "backspace": "\x08",
	"plus": "+", "minus": "-", "equal": "=", "comma": ",", "period": ".",
	"slash": "/", "backslash": "\\", "semicolon": ";", "quote": "'",
	"grave": "`", "leftbracket": "[", "rightbracket": "]",
}

// accelKeyEquivalent converts an accelerator such as "CmdOrCtrl+Shift+S" to
// the key equivalent string and modifier mask of an NSMenuItem. Cmd,
// CmdOrCtrl, Super, Meta and Win are Command; Ctrl is Control. An empty or
// unusable accelerator gives "" and 0.
func accelKeyEquivalent(acc string) (string, uint) {
	acc = strings.TrimSpace(acc)
	if acc == "" {
		return "", 0
	}
	parts := strings.Split(acc, "+")
	key := strings.TrimSpace(parts[len(parts)-1])
	mods := parts[:len(parts)-1]
	if key == "" && len(parts) >= 2 {
		// "Cmd++" is the plus key.
		key = "+"
		mods = parts[:len(parts)-2]
	}
	if key == "" {
		return "", 0
	}
	var mask uint
	for _, p := range mods {
		switch strings.ToLower(strings.TrimSpace(p)) {
		case "cmdorctrl", "commandorcontrol", "cmd", "command", "super", "meta", "win":
			mask |= keyModCommand
		case "ctrl", "control":
			mask |= keyModControl
		case "shift":
			mask |= keyModShift
		case "alt", "option":
			mask |= keyModOption
		}
	}
	lk := strings.ToLower(key)
	if r, ok := menuFunctionKeys[lk]; ok {
		return string(r), mask
	}
	if s, ok := menuControlKeys[lk]; ok {
		return s, mask
	}
	if len(lk) >= 2 && lk[0] == 'f' {
		var n int
		if _, err := fmt.Sscanf(lk[1:], "%d", &n); err == nil && n >= 1 && n <= 20 && fmt.Sprint(n) == lk[1:] {
			return string(rune(0xF703 + n)), mask
		}
	}
	if len([]rune(key)) == 1 {
		return lk, mask
	}
	return "", 0
}

// menuLabel removes mnemonic markers: "&File" is "File", "&&" is a literal &.
func menuLabel(label string) string {
	var b strings.Builder
	for i := 0; i < len(label); i++ {
		if label[i] == '&' {
			if i+1 < len(label) && label[i+1] == '&' {
				b.WriteByte('&')
				i++
			}
			continue
		}
		b.WriteByte(label[i])
	}
	return b.String()
}
