package shell

import (
	"encoding/json"
	"io"
	"runtime"

	"github.com/kratejs/krate/packages/native/platform"
	"github.com/kratejs/krate/packages/native/platform/probe"
)

// DoctorSchema is the version of the krate-shell --doctor JSON.
const DoctorSchema = 1

// DoctorReport is what krate-shell --doctor prints: the shell build and the
// state of the system webview and display.
type DoctorReport struct {
	Schema    int    `json:"schema"`
	Shell     string `json:"shell"`
	GoVersion string `json:"goVersion"`
	probe.Info
}

// Doctor probes this machine. It opens no window.
func Doctor() DoctorReport {
	name := BuildInfo
	if name == "" {
		name = "krate-shell"
	}
	return DoctorReport{Schema: DoctorSchema, Shell: name, GoVersion: runtime.Version(), Info: platform.Probe()}
}

// WriteDoctor writes the report as one line of JSON.
func WriteDoctor(w io.Writer) error {
	return json.NewEncoder(w).Encode(Doctor())
}
