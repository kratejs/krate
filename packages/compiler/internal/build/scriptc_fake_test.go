package build

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A stand-in for scriptc, so the API runtime is tested where scriptc is not
// installed. The test binary is linked as "scriptc" and as the executable
// "scriptc build" writes, and TestMain runs it in that role:
//
//   - coverage reports a blocker at every line of an imported route module
//     that contains "scriptc-blocker", and exits 1 as scriptc does;
//   - build links the test binary at the -o path and records the entry next to
//     it; the executable then runs that entry under node, which strips the
//     types (Node 22.6 or later).
//
// KRATE_FAKE_SCRIPTC_LOG names a file that receives one line per command.

const fakeScriptcEntrySuffix = ".entry"

var fakeImportRe = regexp.MustCompile(`from "([^"]+)"`)

func scriptcFakeMain() (int, bool) {
	switch strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe") {
	case "scriptc":
		return fakeScriptc(os.Args[1:]), true
	case "api-scriptc-server":
		return fakeScriptcServer(), true
	}
	return 0, false
}

func fakeScriptcLog(line string) {
	if p := os.Getenv("KRATE_FAKE_SCRIPTC_LOG"); p != "" {
		if f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			fmt.Fprintln(f, line)
			f.Close()
		}
	}
}

func fakeScriptc(args []string) int {
	if len(args) < 2 {
		return 2
	}
	cmd, entry := args[0], args[1]
	fakeScriptcLog(cmd + " " + filepath.ToSlash(entry))
	switch cmd {
	case "coverage":
		return fakeScriptcCoverage(entry)
	case "build":
		out := ""
		for i, a := range args {
			if a == "-o" && i+1 < len(args) {
				out = args[i+1]
			}
		}
		if out == "" {
			return 2
		}
		if err := linkTestBinary(out); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		_ = os.WriteFile(out+fakeScriptcEntrySuffix, []byte(entry), 0o644)
		_ = os.WriteFile(filepath.Join(filepath.Dir(out), strings.TrimSuffix(filepath.Base(entry), ".ts")+".ll"), []byte("; ir"), 0o644)
		return 0
	}
	return 2
}

func fakeScriptcCoverage(entry string) int {
	src, err := os.ReadFile(entry)
	if err != nil {
		return 2
	}
	var diags []map[string]any
	for _, m := range fakeImportRe.FindAllStringSubmatch(string(src), -1) {
		file := filepath.Join(filepath.Dir(entry), filepath.FromSlash(m[1]))
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		for i, line := range strings.Split(string(data), "\n") {
			if col := strings.Index(line, "scriptc-blocker"); col >= 0 {
				diags = append(diags, map[string]any{
					"code": "SC9999", "category": "unsupported", "severity": "error", "scope": "reached",
					"file": file, "line": i + 1, "column": col + 1,
					"message": "this form is not supported yet", "hint": "rewrite it as the hint suggests",
				})
			}
		}
	}
	doc := map[string]any{
		"schema": "scriptc-diagnostics", "schemaVersion": 1, "command": "coverage", "entry": entry,
		"success": len(diags) == 0, "phase": "complete", "diagnostics": diags,
	}
	if diags == nil {
		doc["diagnostics"] = []any{}
	}
	out, _ := json.Marshal(doc)
	fmt.Println(string(out))
	if len(diags) > 0 {
		return 1
	}
	return 0
}

// fakeScriptcServer is the "compiled" server: node runs the recorded entry.
func fakeScriptcServer() int {
	exe, err := os.Executable()
	if err != nil {
		return 1
	}
	entry, err := os.ReadFile(exe + fakeScriptcEntrySuffix)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	cmd := exec.Command("node", "--experimental-strip-types", "--no-warnings", string(entry))
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return 1
	}
	return 0
}

// linkTestBinary makes path run this test binary: a hard link, else a copy.
func linkTestBinary(path string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	_ = os.Remove(path)
	if os.Link(self, path) == nil {
		return nil
	}
	data, err := os.ReadFile(self)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o755)
}

// fakeScriptcOnPath puts the stand-in first on PATH, with SCRIPTC_LINKER set
// so no linker is needed, and returns the command log path.
func fakeScriptcOnPath(t *testing.T) (logPath string) {
	t.Helper()
	dir := t.TempDir()
	name := "scriptc"
	if os.PathSeparator == '\\' {
		name += ".exe"
	}
	if err := linkTestBinary(filepath.Join(dir, name)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, name), 0o755); err != nil {
		t.Fatal(err)
	}
	logPath = filepath.Join(dir, "scriptc.log")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SCRIPTC_LINKER", "fake")
	t.Setenv("KRATE_FAKE_SCRIPTC_LOG", logPath)
	return logPath
}
