package build

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/environ"
)

// publicEnvDefines returns the compile-time replacements for environment
// reads. Only KRATE_PUBLIC_* values (from .env files and the shell, the shell
// winning) and the build constants are inlined; any other variable stays
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
	add := func(k, v string) {
		if !strings.HasPrefix(k, bundler.PublicEnvPrefix) {
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
