// Command krate-memtree prints the memory of a process and all its
// descendants as one JSON document, using the same numbers as
// `krate native profile --memory` (packages/native/memstat). The native
// benchmark harness uses it for Krate, Electron and Tauri apps alike.
//
//	krate-memtree --pid <pid> [--extra <pid>,<pid>...]
//
// --extra adds processes that are not descendants, such as the WebKit XPC
// services of a macOS app.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/native/memstat"
)

func parsePIDs(s string) ([]int, error) {
	var out []int
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' }) {
		n, err := strconv.Atoi(f)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("bad pid %q", f)
		}
		out = append(out, n)
	}
	return out, nil
}

func run() error {
	pid := flag.Int("pid", 0, "root process")
	extra := flag.String("extra", "", "more pids, comma separated")
	flag.Parse()
	if *pid <= 0 {
		return fmt.Errorf("--pid is required")
	}
	extras, err := parsePIDs(*extra)
	if err != nil {
		return err
	}
	desc, err := memstat.Descendants(*pid)
	if err != nil {
		return err
	}
	targets := []memstat.Target{{PID: *pid, Role: "root"}}
	for _, p := range desc {
		targets = append(targets, memstat.Target{PID: p, Role: "child"})
	}
	for _, p := range extras {
		targets = append(targets, memstat.Target{PID: p, Role: "extra"})
	}
	rep, err := memstat.Collect(targets)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(rep)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "krate-memtree:", err)
		os.Exit(1)
	}
}
