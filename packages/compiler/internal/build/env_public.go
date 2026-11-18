package build

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/environ"
)

// publicEnvPrefixes returns the name prefixes of the variables that are
// inlined: config.envPrefix, or KRATE_PUBLIC_.
func (b *Builder) publicEnvPrefixes() []string {
	return b.Cfg.PublicEnvPrefixes()
}

func hasAnyPrefix(name string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// publicEnvDefines returns the compile-time replacements for environment
// reads. Only values whose names start with a public prefix (KRATE_PUBLIC_ by
// default; from .env files and the shell, the shell winning) and the build
// constants are inlined; any other variable stays
// server-side and never reaches client output.
func (b *Builder) publicEnvDefines() map[string]string {
	mode := environ.Mode("production")
	if b.DevMode {
		mode = environ.Mode("development")
	}
	quote := func(s string) string {
		out, _ := json.Marshal(s)
		return string(out)
	}
	base := b.Cfg.BaseURLPath()
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	defs := map[string]string{}
	for _, prefix := range []string{"import.meta.env.", "process.env."} {
		defs[prefix+"MODE"] = quote(mode)
		defs[prefix+"DEV"] = boolJS(mode != "production")
		defs[prefix+"PROD"] = boolJS(mode == "production")
		defs[prefix+"BASE_URL"] = quote(base)
		defs[prefix+"SSR"] = "false"
	}
	defs["process.env.NODE_ENV"] = quote(nodeEnvFor(mode))
	defs["import.meta.env.NODE_ENV"] = quote(nodeEnvFor(mode))
	prefixes := b.publicEnvPrefixes()
	add := func(k, v string) {
		if !hasAnyPrefix(k, prefixes) {
			return
		}
		defs["import.meta.env."+k] = quote(v)
		defs["process.env."+k] = quote(v)
	}
	for k, v := range b.Env {
		add(k, v)
	}
	for k, v := range environ.Current {
		if _, ok := b.Env[k]; !ok {
			add(k, v)
		}
	}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			add(k, v)
		}
	}
	return defs
}

// clientEnvDefines is publicEnvDefines plus an empty process.env, for esbuild
// bundles that ship to the browser: set KRATE_PUBLIC_* values and NODE_ENV are
// inlined, and every other process.env read (project or library code) yields
// undefined instead of throwing because process does not exist.
func (b *Builder) clientEnvDefines() map[string]string {
	defs := b.publicEnvDefines()
	defs["process.env"] = "{}"
	return defs
}

func (b *Builder) vendorDefines(mode string) map[string]string {
	defs := b.clientEnvDefines()
	defs["process.env.NODE_ENV"] = strconv.Quote(mode)
	return defs
}

func boolJS(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func nodeEnvFor(mode string) string {
	if mode == "development" {
		return "development"
	}
	return "production"
}

// neutralizeClientEnv keeps unexposed environment reads from breaking the
// classic hydration script: import.meta is a syntax error there and process is
// undefined. Reads were already reported as KR8310 at the source location.
func neutralizeClientEnv(js string) string {
	if strings.Contains(js, "import.meta.env") {
		js = strings.ReplaceAll(js, "import.meta.env", "({})")
	}
	if strings.Contains(js, "process.env") {
		js = strings.ReplaceAll(js, "process.env", "({})")
	}
	return js
}

// scriptEnv is the process.env an embedded build-time VM sees: the shell
// environment overlaid with the project's .env values, the shell winning.
func (b *Builder) scriptEnv() map[string]string {
	return environ.Merge(b.Env)
}

// envFingerprint hashes the environment a page can be built from: the inlined
// public values and build constants, plus the .env file values that
// build-time code (generateStaticParams, server components) can read. Shell
// variables that are not public are left out so unrelated ones (PWD, SHLVL) do
// not defeat the build cache.
func (b *Builder) envFingerprint() string {
	h := sha256.New()
	defs := b.publicEnvDefines()
	keys := make([]string, 0, len(defs))
	for k := range defs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		h.Write([]byte(k + "\x00" + defs[k] + "\x00"))
	}
	h.Write([]byte{1})
	h.Write([]byte(strings.Join(b.publicEnvPrefixes(), ",")))
	h.Write([]byte{1})
	for _, kv := range environ.KVList(b.Env) {
		h.Write([]byte(kv + "\x00"))
	}
	// Pages carry the base path in asset and link URLs.
	h.Write([]byte{1})
	h.Write([]byte(b.Cfg.BaseURLPath()))
	return hex.EncodeToString(h.Sum(nil))
}

// envIsSet reports whether name has a value in the shell or the .env files.
// An empty value counts as set, as it does for inlining.
func (b *Builder) envIsSet(name string) bool {
	if _, ok := b.Env[name]; ok {
		return true
	}
	if _, ok := environ.Current[name]; ok {
		return true
	}
	_, ok := os.LookupEnv(name)
	return ok
}

// envFilesHint names the directory and files the .env values are read from.
func (b *Builder) envFilesHint() string {
	dir := b.Root
	if environ.Dir != "" {
		dir = environ.Dir
	} else if b.Cfg != nil && b.Cfg.EnvDir != "" {
		dir = b.Cfg.EnvDir
	}
	shown := "."
	if rel, err := filepath.Rel(b.Root, dir); err == nil && rel != "" {
		shown = filepath.ToSlash(rel)
	}
	mode := "production"
	if b.DevMode {
		mode = "development"
	}
	prefix := ""
	if shown != "." {
		prefix = shown + "/"
	}
	return "Define it in " + prefix + ".env (Krate loads .env, .env." + mode + ", .env.local, .env." + mode + ".local from " + shown + ") or export it in the build environment"
}

// envNotExposedHint words the KR8310 hint for the variable a client read
// names, given the configured public prefixes and what is actually set.
func (b *Builder) envNotExposedHint(d diag.Diagnostic) string {
	name := d.EnvName
	if name == "" {
		return d.Hint
	}
	prefixes := b.publicEnvPrefixes()
	cmd := "krate build"
	if b.DevMode {
		cmd = "krate dev"
	}
	// Prefixes a project is likely to carry over from another tool.
	known := append([]string(nil), prefixes...)
	for _, p := range []string{config.DefaultEnvPrefix, "NEXT_PUBLIC_"} {
		if !containsString(known, p) {
			known = append(known, p)
		}
	}
	for _, p := range prefixes {
		if !strings.HasPrefix(name, p) {
			continue
		}
		rest := name[len(p):]
		for _, q := range known {
			if q == p {
				continue
			}
			twin := q + rest
			if !b.envIsSet(twin) {
				continue
			}
			if containsString(prefixes, q) {
				return twin + " is set but the code reads " + name + ". Read " + twin + " instead, or define " + name
			}
			return twin + " is set but the code reads " + name + ". Rename one of them, or add \"" + q + "\" to envPrefix in krate.config.ts"
		}
		return name + " is not set when " + cmd + " runs. " + b.envFilesHint()
	}
	// A prefixed name that is not a configured public prefix.
	for _, q := range known {
		if strings.HasPrefix(name, q) && !containsString(prefixes, q) {
			return q + " is not a public prefix in this project; add \"" + q + "\" to envPrefix in krate.config.ts, or rename the variable to " + config.DefaultEnvPrefix + name[len(q):]
		}
	}
	hint := d.Hint
	for _, q := range prefixes {
		if twin := q + name; b.envIsSet(twin) {
			hint += " (" + twin + " is set: use that name)"
			break
		}
	}
	return hint
}
