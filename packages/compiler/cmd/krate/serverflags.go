package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// parsePortValue validates a --port / PORT value.
func parsePortValue(source, v string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 1 || n > 65535 {
		return 0, fmt.Errorf("invalid %s %q: expected an integer between 1 and 65535", source, v)
	}
	return n, nil
}

// applyServerFlags layers the listen address onto cfg for the commands that
// start an HTTP server (dev, serve, preview, start). Precedence for the port:
// --port flag, then KRATE_SERVE_PORT, then PORT (serve/start only), then the
// config file (server.port / devServer.port). The host comes from --host, then
// KRATE_SERVE_HOST, then server.host.
func applyServerFlags(command string, flags cliFlags, cfg *config.Config, getenv func(string) string) error {
	switch command {
	case "dev", "serve", "preview", "start":
	default:
		return nil
	}
	production := command != "dev"
	port, from := 0, ""
	switch {
	case flags.PortSet:
		port, from = flags.Port, "--port"
	case production && getenv("KRATE_SERVE_PORT") != "":
		port, from = -1, "KRATE_SERVE_PORT"
	case production && getenv("PORT") != "":
		port, from = -1, "PORT"
	}
	if port == -1 {
		n, err := parsePortValue(from, getenv(from))
		if err != nil {
			return err
		}
		port = n
	}
	if from != "" {
		cfg.Server.Port = port
		if command == "dev" {
			cfg.DevServer.Port = port
		}
	}
	switch {
	case flags.Host != "":
		cfg.Server.Host = flags.Host
	case production && getenv("KRATE_SERVE_HOST") != "":
		cfg.Server.Host = getenv("KRATE_SERVE_HOST")
	}
	return nil
}
