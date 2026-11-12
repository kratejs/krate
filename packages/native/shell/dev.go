package shell

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/kratejs/krate/packages/native/sdk"
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

// ViewQuerier is the optional part of Controller behind the views, query-in
// and emit commands (the test driver's).
type ViewQuerier interface {
	QueryView(view, js string) (string, error)
	Views() []sdk.ViewInfo
	Emit(area, payload string) error
}

// ServeControl reads newline-terminated commands from r and answers each with
// one line on w, "ok" or "error <message>". It returns when r ends or after
// "quit". Commands:
//
//	reload-hostjs
//	restart-gohost
//	eval <js>
//	query <js>     answers "ok <json>"; needs a Controller that implements Querier
//	views          answers "ok <json>": [{"id","window","primary"}], one per open view
//	query-in <view> <js>   query, in the one view with this SDK id
//
// For query and query-in the JavaScript may be written "@json <string>",
// where the string is a JSON string literal holding the source; that is how
// source with line breaks travels on one line.
//
//	emit <area> <json>     deliver a tray, menu or shortcut event to the app
//	memory         answers "ok <json>" (a memstat.Report); needs a MemorySampler
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
		case "query":
			q, ok := ctl.(Querier)
			switch {
			case strings.TrimSpace(arg) == "":
				reply(fmt.Errorf("query needs a JavaScript expression"))
			case !ok:
				reply(fmt.Errorf("query is not supported here"))
			default:
				js, err := jsArg(arg)
				if err != nil {
					reply(err)
					continue
				}
				v, err := q.Query(js)
				if err != nil {
					reply(err)
				} else {
					fmt.Fprintf(w, "ok %s\n", oneLine(v))
				}
			}
		case "views":
			vq, ok := ctl.(ViewQuerier)
			if !ok {
				reply(fmt.Errorf("views is not supported here"))
				continue
			}
			list := vq.Views()
			if list == nil {
				list = []sdk.ViewInfo{}
			}
			data, jerr := json.Marshal(list)
			if jerr != nil {
				reply(jerr)
				continue
			}
			fmt.Fprintf(w, "ok %s\n", oneLine(string(data)))
		case "query-in":
			vq, ok := ctl.(ViewQuerier)
			view, expr, _ := strings.Cut(strings.TrimSpace(arg), " ")
			switch {
			case !ok:
				reply(fmt.Errorf("query-in is not supported here"))
			case view == "" || strings.TrimSpace(expr) == "":
				reply(fmt.Errorf("query-in needs a view id and a JavaScript expression"))
			default:
				js, err := jsArg(expr)
				if err != nil {
					reply(err)
					continue
				}
				v, err := vq.QueryView(view, js)
				if err != nil {
					reply(err)
				} else {
					fmt.Fprintf(w, "ok %s\n", oneLine(v))
				}
			}
		case "emit":
			vq, ok := ctl.(ViewQuerier)
			area, payload, _ := strings.Cut(strings.TrimSpace(arg), " ")
			switch {
			case !ok:
				reply(fmt.Errorf("emit is not supported here"))
			case area == "":
				reply(fmt.Errorf("emit needs an area (tray, menu or shortcut) and a JSON payload"))
			default:
				reply(vq.Emit(area, payload))
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

// jsArg returns the JavaScript of a query argument: the text itself, or the
// decoded string of "@json <json string>".
func jsArg(arg string) (string, error) {
	rest, ok := strings.CutPrefix(arg, "@json ")
	if !ok {
		return arg, nil
	}
	var src string
	if err := json.Unmarshal([]byte(rest), &src); err != nil {
		return "", fmt.Errorf("@json needs a JSON string: %v", err)
	}
	if strings.TrimSpace(src) == "" {
		return "", fmt.Errorf("query needs a JavaScript expression")
	}
	return src, nil
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
