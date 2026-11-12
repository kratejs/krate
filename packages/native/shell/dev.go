package shell

import (
	"bufio"
	"encoding/json"
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

// Querier is the optional part of Controller behind the query command.
type Querier interface {
	Query(js string) (string, error)
}

// Dropper is the optional part of Controller behind the drop command.
type Dropper interface {
	Drop(paths []string) error
}

// ServeControl reads newline-terminated commands from r and answers each with
// one line on w, "ok" or "error <message>". It returns when r ends or after
// "quit". Commands:
//
//	reload-hostjs
//	restart-gohost
//	eval <js>
//	query <js>     answers "ok <json>"; needs a Controller that implements Querier
//	memory         answers "ok <json>" (a memstat.Report); needs a MemorySampler
//	hmr <json>
//	drop <path>[\x1f<path>...]  report a drop of files onto the main window, as the platform does
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
		case "query":
			q, ok := ctl.(Querier)
			switch {
			case strings.TrimSpace(arg) == "":
				reply(fmt.Errorf("query needs a JavaScript expression"))
			case !ok:
				reply(fmt.Errorf("query is not supported here"))
			default:
				v, err := q.Query(arg)
				if err != nil {
					reply(err)
				} else {
					fmt.Fprintf(w, "ok %s\n", oneLine(v))
				}
			}
		case "memory":
			m, ok := ctl.(MemorySampler)
			if !ok {
				reply(fmt.Errorf("memory is not supported here"))
				continue
			}
			rep, err := m.Memory()
			if rep == nil {
				reply(err)
				continue
			}
			data, jerr := json.Marshal(rep)
			if jerr != nil {
				reply(jerr)
				continue
			}
			fmt.Fprintf(w, "ok %s\n", oneLine(string(data)))
		case "drop":
			d, ok := ctl.(Dropper)
			switch {
			case strings.TrimSpace(arg) == "":
				reply(fmt.Errorf("drop needs file paths"))
			case !ok:
				reply(fmt.Errorf("drop is not supported here"))
			default:
				reply(d.Drop(strings.Split(arg, "\x1f")))
			}
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
