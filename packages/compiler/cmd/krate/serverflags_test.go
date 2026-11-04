package main

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestApplyServerFlagsPrecedence(t *testing.T) {
	cases := []struct {
		name    string
		command string
		flags   cliFlags
		env     map[string]string
		cfgPort int
		want    int
		wantErr bool
	}{
		{name: "config only", command: "serve", cfgPort: 4000, want: 4000},
		{name: "PORT beats config", command: "serve", env: map[string]string{"PORT": "5000"}, cfgPort: 4000, want: 5000},
		{name: "KRATE_SERVE_PORT beats PORT", command: "start", env: map[string]string{"PORT": "5000", "KRATE_SERVE_PORT": "6000"}, cfgPort: 4000, want: 6000},
		{name: "flag beats env", command: "serve", flags: cliFlags{Port: 7000, PortSet: true}, env: map[string]string{"KRATE_SERVE_PORT": "6000"}, cfgPort: 4000, want: 7000},
		{name: "dev ignores PORT env", command: "dev", env: map[string]string{"PORT": "5000"}, cfgPort: 4000, want: 4000},
		{name: "dev takes flag", command: "dev", flags: cliFlags{Port: 7001, PortSet: true}, cfgPort: 4000, want: 7001},
		{name: "mcp untouched", command: "mcp", flags: cliFlags{Port: 7331, PortSet: true}, cfgPort: 4000, want: 4000},
		{name: "bad env", command: "serve", env: map[string]string{"PORT": "abc"}, wantErr: true},
		{name: "out of range env", command: "serve", env: map[string]string{"KRATE_SERVE_PORT": "70000"}, wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Server.Port = c.cfgPort
			err := applyServerFlags(c.command, c.flags, cfg, envMap(c.env))
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if !c.wantErr && cfg.ServerPort() != orDefault(c.want, 3000) {
				t.Fatalf("port = %d, want %d", cfg.ServerPort(), c.want)
			}
		})
	}
}

func orDefault(v, d int) int {
	if v == 0 {
		return d
	}
	return v
}

func TestApplyServerFlagsHost(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Host = "10.0.0.1"
	if err := applyServerFlags("serve", cliFlags{Host: "0.0.0.0"}, cfg, envMap(nil)); err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Host != "0.0.0.0" {
		t.Fatalf("host = %q", cfg.Server.Host)
	}
	cfg2 := &config.Config{}
	if err := applyServerFlags("start", cliFlags{}, cfg2, envMap(map[string]string{"KRATE_SERVE_HOST": "127.0.0.2"})); err != nil {
		t.Fatal(err)
	}
	if cfg2.Server.Host != "127.0.0.2" {
		t.Fatalf("host = %q", cfg2.Server.Host)
	}
}

func TestParseFlagsPortValidation(t *testing.T) {
	flags, rest, err := parseFlagsChecked([]string{"serve", "--port", "8123", "--host", "0.0.0.0"})
	if err != nil || flags.Port != 8123 || !flags.PortSet || flags.Host != "0.0.0.0" || len(rest) != 1 {
		t.Fatalf("flags = %+v rest = %v err = %v", flags, rest, err)
	}
	flags, _, err = parseFlagsChecked([]string{"serve", "--port=9001"})
	if err != nil || flags.Port != 9001 {
		t.Fatalf("--port=N: %+v %v", flags, err)
	}
	for _, bad := range [][]string{{"serve", "--port", "abc"}, {"serve", "--port"}, {"serve", "--port", "0"}, {"serve", "--host"}} {
		if _, _, err := parseFlagsChecked(bad); err == nil {
			t.Errorf("%v: expected an error", bad)
		}
	}
}
