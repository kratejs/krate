package shell

import (
	"bufio"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// Controller is what the dev control channel drives; *Control implements it.
type Controller interface {
	ReloadHostJS() error
	RestartGoHost() error
	Eval(js string) error
	HMR(msg string) error
	Quit()
}

// ServeControl reads newline-terminated commands from r and answers each with
// one line on w, "ok" or "error <message>". It returns when r ends or after
// "quit". Commands:
//
//	reload-hostjs
//	restart-gohost
//	eval <js>
//	hmr <json>
//	quit
//
// It returns true if it stopped because of "quit".
func ServeControl(r io.Reader, w io.Writer, ctl Controller) (quit bool) {
	reply := func(err error) {
		if err != nil {
			fmt.Fprintf(w, "error %s\n", oneLine(err.Error()))
			return
		}
		fmt.Fprintln(w, "ok")
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 16<<20)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		cmd, arg, _ := strings.Cut(line, " ")
		switch cmd {
		case "":
			continue
		case "reload-hostjs":
			reply(ctl.ReloadHostJS())
		case "restart-gohost":
			reply(ctl.RestartGoHost())
		case "eval":
			if strings.TrimSpace(arg) == "" {
				reply(fmt.Errorf("eval needs JavaScript"))
				continue
			}
			reply(ctl.Eval(arg))
		case "hmr":
			if strings.TrimSpace(arg) == "" {
				reply(fmt.Errorf("hmr needs a JSON message"))
				continue
			}
			reply(ctl.HMR(arg))
		case "quit":
			reply(nil)
			ctl.Quit()
			return true
		default:
			reply(fmt.Errorf("unknown command %q", cmd))
		}
	}
	return false
}

func oneLine(s string) string {
	return strings.NewReplacer("\r\n", " | ", "\n", " | ", "\r", " ").Replace(s)
}

// DefaultAppDir is where a release build keeps the app next to the shell
// binary: <dir of exe>/app, or ../Resources/app inside a macOS .app bundle
// (Contents/MacOS/<exe>).
func DefaultAppDir(exe string) string {
	dir := dirOf(exe)
	if base(dir) == "MacOS" && base(parent(dir)) == "Contents" {
		return join(parent(dir), "Resources", "app")
	}
	return join(dir, "app")
}

func dirOf(p string) string       { return filepath.Dir(p) }
func base(p string) string        { return filepath.Base(p) }
func parent(p string) string      { return filepath.Dir(p) }
func join(parts ...string) string { return filepath.Join(parts...) }
