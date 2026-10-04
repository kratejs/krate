package build

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/pluginapi"
)

// writeBuildError serialises a build-error event (structured diagnostics) as an
// SSE frame. The client overlay renders the diagnostics with source + caret.
func writeBuildError(w io.Writer, ev DevEvent) {
	payload := struct {
		Diagnostics []interface{} `json:"diagnostics"`
		BuildOK     bool          `json:"buildOk"`
	}{BuildOK: len(ev.Diagnostics) == 0}
	for _, d := range ev.Diagnostics {
		payload.Diagnostics = append(payload.Diagnostics, d)
	}
	data, _ := json.Marshal(payload)
	fmt.Fprintf(w, "event: build-error\ndata: %s\n\n", data)
}

// safeProjectPath resolves a client-supplied file path (absolute or relative)
// against the project root and rejects anything that escapes it or is not an
// existing regular file. Diagnostics carry absolute page paths, so absolute
// paths inside the root must be accepted.
func safeProjectPath(root, file string) (string, bool) {
	if file == "" {
		return "", false
	}
	p := filepath.Clean(filepath.FromSlash(file))
	var abs string
	if filepath.IsAbs(p) {
		abs = p
	} else {
		abs = filepath.Join(root, p)
	}
	if !pluginapi.WithinRoot(root, abs) {
		return "", false
	}
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		return "", false
	}
	return abs, true
}

// openInEditor launches the configured editor at file:line. The editor command
// may include flags (e.g. "code -g"); when empty, VS Code's `code` is assumed.
// Launch is non-blocking so the request returns immediately.
func openInEditor(editor, file, line string) error {
	if editor == "" {
		editor = "code"
	}
	fields := strings.Fields(editor)
	if len(fields) == 0 {
		fields = []string{"code"}
	}
	name := fields[0]
	args := append([]string{}, fields[1:]...)
	if line != "" && line != "0" {
		args = append(args, "-g", file+":"+line)
	} else {
		args = append(args, file)
	}
	cmd := exec.Command(name, args...)
	return cmd.Start()
}
