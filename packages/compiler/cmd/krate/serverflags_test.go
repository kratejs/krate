package main

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// An explicit `krate dev --port` is a fixed port: a busy one is an error, not
// a silent move. A port from the config or none at all still auto-moves.
func TestDevPortFlagFixesThePort(t *testing.T) {
	t.Parallel()
	noEnv := func(string) string { return "" }

	cfg := config.Default()
	if err := applyServerFlags("dev", cliFlags{Port: 4100, PortSet: true}, cfg, noEnv); err != nil {
		t.Fatal(err)
	}
	if cfg.DevPort() != 4100 || !cfg.DevServer.PortFixed {
		t.Errorf("--port: port %d fixed %v, want 4100 true", cfg.DevPort(), cfg.DevServer.PortFixed)
	}

	cfg = config.Default()
	cfg.DevServer.Port = 4200
	if err := applyServerFlags("dev", cliFlags{}, cfg, noEnv); err != nil {
		t.Fatal(err)
	}
	if cfg.DevPort() != 4200 || cfg.DevServer.PortFixed {
		t.Errorf("config port: port %d fixed %v, want 4200 false", cfg.DevPort(), cfg.DevServer.PortFixed)
	}

	// The environment is not read by dev, so it never fixes the port.
	cfg = config.Default()
	env := func(k string) string {
		if k == "PORT" || k == "KRATE_SERVE_PORT" {
			return "4300"
		}
		return ""
	}
	if err := applyServerFlags("dev", cliFlags{}, cfg, env); err != nil {
		t.Fatal(err)
	}
	if cfg.DevServer.PortFixed || cfg.DevPort() != 3000 {
		t.Errorf("env: port %d fixed %v, want 3000 false", cfg.DevPort(), cfg.DevServer.PortFixed)
	}
}
