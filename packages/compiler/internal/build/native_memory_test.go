package build

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// The memory measurement is driven through the shell's dev control channel.
// These tests run it against a scripted stand-in (this test binary, started
// with KRATE_FAKE_SHELL set), so they need no display. The real thing runs in
// TestNativeBudgetGUI and `krate native profile --memory`.

func TestMain(m *testing.M) {
	if mode := os.Getenv("KRATE_FAKE_SHELL"); mode != "" {
		os.Exit(fakeShell(mode))
	}
	os.Exit(m.Run())
}

// fakeShell speaks the dev control protocol. KRATE_FAKE_LOG names a file that
// receives every command it was sent.
func fakeShell(mode string) int {
	logf, _ := os.OpenFile(os.Getenv("KRATE_FAKE_LOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if logf != nil {
		defer logf.Close()
	}
	fmt.Println("ready")
	// PSS and private dirty in MB per role: the app has a Go host, the floor
	// does not, and the app's web process grows after a navigation.
	base := map[string][2]int{"shell": {80, 40}, "webview:web": {100, 38}, "webview:network": {45, 25}}
	if mode == "app" {
		base["gohost"] = [2]int{10, 4}
		base["shell"] = [2]int{83, 41}
	}
	navs := 0
	hidden := false
	sc := bufioScanner(os.Stdin)
	for sc.Scan() {
		line := sc.Text()
		if logf != nil {
			fmt.Fprintln(logf, mode+" "+line)
		}
		cmd, _, _ := strings.Cut(line, " ")
		switch cmd {
		case "query":
			switch {
			case strings.Contains(line, "krate/window.hide"):
				hidden = true
				fmt.Println(`ok "ok"`)
			default:
				fmt.Println("ok true")
			}
		case "eval":
			navs++
			fmt.Println("ok")
		case "memory":
			type proc struct {
				PID          int    `json:"pid"`
				Name         string `json:"name"`
				Role         string `json:"role"`
				PSS          uint64 `json:"pss"`
				PrivateDirty uint64 `json:"privateDirty"`
			}
			var procs []proc
			pid := 100
			for role, v := range base {
				pss, pd := v[0], v[1]
				if role == "webview:web" {
					pss += navs / 2
				}
				if hidden && role == "webview:web" {
					pss -= 20
				}
				pid++
				procs = append(procs, proc{pid, role, role, uint64(pss) << 20, uint64(pd) << 20})
			}
			// A second web process: roles are summed.
			procs = append(procs, proc{999, "web2", "webview:web", 5 << 20, 1 << 20})
			data, _ := json.Marshal(map[string]any{"os": "linux", "source": "smaps_rollup", "procs": procs, "notes": []string{"fake note"}})
			fmt.Println("ok " + string(data))
		case "quit":
			fmt.Println("ok")
			return 0
		default:
			fmt.Println("error unknown command")
		}
	}
	return 0
}

func bufioScanner(f *os.File) *bufio.Scanner {
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 16<<20)
	return sc
}
