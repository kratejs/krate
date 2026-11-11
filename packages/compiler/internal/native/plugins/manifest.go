// Package plugins is the compiler side of Krate Native plugins: it reads and
// checks krate-plugin.json, resolves the entries of native.plugins to
// directories, generates the glue and TypeScript module of each plugin with
// gobind, and writes the Go module of the per-app shell that links them. The
// runtime side is packages/native/plugin.
package plugins

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// ManifestFile is the name of a plugin's manifest.
const ManifestFile = "krate-plugin.json"

// Manifest is krate-plugin.json.
type Manifest struct {
	// Name is the plugin's name: lowercase words joined by hyphens. Its ops
	// are the host ids "plugin/<name>.<op>".
	Name        string `json:"name"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
	// Krate is the oldest Krate version the plugin works with, "0.5.0" or
	// ">=0.5.0".
	Krate string `json:"krate,omitempty"`
	// Go is the import path of the plugin's Go package, the directory that
	// holds this manifest.
	Go string `json:"go"`
	// TS is an optional TypeScript entry (relative to the manifest) that
	// the plugin's npm package ships next to the generated module.
	TS          string       `json:"ts,omitempty"`
	Permissions []Permission `json:"permissions,omitempty"`
	Ops         []Op         `json:"ops"`
}

// Permission declares a permission of the plugin.
type Permission struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Op declares an op of the plugin.
type Op struct {
	// Name is the op's name, lowerCamel like the SDK ops ("info", "cpuLoad"):
	// the host id is plugin/<plugin>.<name> and the generated module exports
	// it. It is bound to the exported Go function or *krate.Signal variable
	// Go (default: Name with its first letter upper-cased, "info" is Info), or,
	// with Manual, to an op the plugin registers itself from Init.
	Name string `json:"name"`
	// Go is the exported Go name when it is not Name with an upper-case first
	// letter, for example "CPULoad" for "cpuLoad".
	Go          string `json:"go,omitempty"`
	Permission  string `json:"permission,omitempty"`
	Manual      bool   `json:"manual,omitempty"`
	Description string `json:"description,omitempty"`
}

var (
	nameRe   = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	opRe     = regexp.MustCompile(`^[a-z][A-Za-z0-9]*$`)
	goNameRe = regexp.MustCompile(`^[A-Z][A-Za-z0-9_]*$`)
	permRe   = regexp.MustCompile(`^[a-z][A-Za-z0-9]*$`)
	importRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~-]*(/[A-Za-z0-9._~-]+)*$`)
)

// ValidName reports whether s can be a plugin name.
func ValidName(s string) bool { return len(s) >= 2 && len(s) <= 40 && nameRe.MatchString(s) }

// PermissionName is the app.json name of a plugin permission.
func PermissionName(plugin, local string) string { return "plugin:" + plugin + "." + local }

// GoName is the exported Go name an op binds to.
func (o Op) GoName() string {
	if o.Go != "" {
		return o.Go
	}
	if o.Name == "" {
		return ""
	}
	return strings.ToUpper(o.Name[:1]) + o.Name[1:]
}

// OpID is the host id of an op.
func OpID(plugin, op string) string { return "plugin/" + plugin + "." + op }

// ParseManifest parses and validates a manifest. file is used in diagnostics.
func ParseManifest(data []byte, file string) (*Manifest, []diag.Diagnostic) {
	bad := func(code diag.Code, msg, hint string) []diag.Diagnostic {
		return []diag.Diagnostic{diag.New(file, 1, 1, msg, hint, "").WithCode(code)}
	}
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, bad(diag.CodePluginManifestInvalid, "krate-plugin.json: "+err.Error(),
			"the manifest has the fields name, go, version, krate, ts, permissions and ops; see the plugin docs")
	}
	var ds []diag.Diagnostic
	add := func(code diag.Code, msg, hint string) {
		ds = append(ds, diag.New(file, 1, 1, msg, hint, "").WithCode(code))
	}
	if !ValidName(m.Name) {
		add(diag.CodePluginManifestInvalid, fmt.Sprintf("name %q is not a valid plugin name", m.Name),
			"use lowercase words joined by hyphens, 2 to 40 characters, for example \"system-info\"")
	}
	if m.Go == "" || !importRe.MatchString(m.Go) {
		add(diag.CodePluginManifestInvalid, fmt.Sprintf("go %q is not a Go import path", m.Go),
			"set \"go\" to the import path of the plugin's package, for example \"github.com/acme/krate-sqlite\"")
	}
	if m.Krate != "" {
		if _, ok := ParseVersion(m.Krate); !ok {
			add(diag.CodePluginManifestInvalid, fmt.Sprintf("krate %q is not a version", m.Krate), "use \"0.5.0\" or \">=0.5.0\"")
		}
	}
	if m.TS != "" && (strings.HasPrefix(m.TS, "/") || strings.Contains(m.TS, "..")) {
		add(diag.CodePluginManifestInvalid, fmt.Sprintf("ts %q must be a relative path inside the plugin", m.TS), "")
	}
	perms := map[string]bool{}
	for i, p := range m.Permissions {
		switch {
		case !permRe.MatchString(p.Name):
			add(diag.CodePluginManifestInvalid, fmt.Sprintf("permissions[%d]: name %q is not valid", i, p.Name),
				"use lowerCamel names, for example \"read\" or \"readAll\"")
		case perms[p.Name]:
			add(diag.CodePluginDuplicate, fmt.Sprintf("permission %q is declared twice", p.Name), "rename or remove one of them")
		}
		perms[p.Name] = true
	}
	ops := map[string]bool{}
	goNames := map[string]string{}
	for i, o := range m.Ops {
		switch {
		case !opRe.MatchString(o.Name):
			add(diag.CodePluginManifestInvalid, fmt.Sprintf("ops[%d]: name %q is not valid", i, o.Name),
				"op names are lowerCamel like the SDK ops: \"info\", \"cpuLoad\" (the Go function is Info, CPULoad; set \"go\" for a name that is not the capitalized op)")
		case ops[o.Name]:
			add(diag.CodePluginDuplicate, fmt.Sprintf("op %q is declared twice, so the host id %s would be ambiguous", o.Name, OpID(m.Name, o.Name)),
				"each op name must be unique within the plugin")
		}
		ops[o.Name] = true
		if o.Go != "" && !goNameRe.MatchString(o.Go) {
			add(diag.CodePluginManifestInvalid, fmt.Sprintf("op %q: go %q is not an exported Go identifier", o.Name, o.Go), "use the name of the exported function or signal, for example \"CPULoad\"")
		} else if o.Name != "" && opRe.MatchString(o.Name) {
			if prev, dup := goNames[o.GoName()]; dup {
				add(diag.CodePluginDuplicate, fmt.Sprintf("ops %q and %q both bind to the Go name %s", prev, o.Name, o.GoName()), "set \"go\" on one of them")
			}
			goNames[o.GoName()] = o.Name
		}
		if o.Permission != "" && !perms[o.Permission] {
			add(diag.CodePluginManifestInvalid, fmt.Sprintf("op %q needs permission %q, which the manifest does not declare", o.Name, o.Permission),
				"add {\"name\": \""+o.Permission+"\", \"description\": \"...\"} to \"permissions\"")
		}
	}
	if len(ds) > 0 {
		return nil, ds
	}
	return &m, nil
}

// Version is a parsed major.minor.patch.
type Version [3]int

// ParseVersion reads "1.2.3", "v1.2.3", ">=1.2.3" or "^1.2.3" (a prerelease or
// build suffix is ignored).
func ParseVersion(s string) (Version, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimLeft(s, ">=^~v ")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return Version{}, false
	}
	var v Version
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return Version{}, false
		}
		v[i] = n
	}
	return v, true
}

// Less reports whether v is older than w.
func (v Version) Less(w Version) bool {
	for i := range v {
		if v[i] != w[i] {
			return v[i] < w[i]
		}
	}
	return false
}
