package build

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSSRRuntimeCommand(t *testing.T) {
	staged := filepath.Join("dist", ".krate", "server-renderer.mjs")
	rawTS := filepath.Join("src", "server-renderer.ts")

	tests := []struct {
		name        string
		runtime     string
		renderer    string
		wantCmd     string
		wantArgsSub []string // substrings each must appear in args
	}{
		{
			name:        "node staged driver",
			runtime:     "node",
			renderer:    staged,
			wantCmd:     "node",
			wantArgsSub: []string{staged},
		},
		{
			name:        "empty defaults to node",
			runtime:     "",
			renderer:    staged,
			wantCmd:     "node",
			wantArgsSub: []string{staged},
		},
		{
			name:        "node legacy ts source uses tsx",
			runtime:     "node",
			renderer:    rawTS,
			wantCmd:     "npx",
			wantArgsSub: []string{"tsx", rawTS},
		},
		{
			name:        "bun",
			runtime:     "bun",
			renderer:    staged,
			wantCmd:     "bun",
			wantArgsSub: []string{"run", staged},
		},
		{
			name:        "deno gets network/fs/env flags",
			runtime:     "deno",
			renderer:    staged,
			wantCmd:     "deno",
			wantArgsSub: []string{"run", "--allow-net", "--allow-read", "--allow-env", "--allow-sys", staged},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, args, err := ssrRuntimeCommand(tt.runtime, tt.renderer)
			if err != nil {
				t.Fatalf("ssrRuntimeCommand: %v", err)
			}
			if cmd != tt.wantCmd {
				t.Errorf("cmd = %q, want %q", cmd, tt.wantCmd)
			}
			joined := strings.Join(args, " ")
			for _, sub := range tt.wantArgsSub {
				if !strings.Contains(joined, sub) {
					t.Errorf("args %q missing %q", joined, sub)
				}
			}
		})
	}

	if _, _, err := ssrRuntimeCommand("python", staged); err == nil {
		t.Error("expected error for unsupported runtime")
	}
}

func TestNewSSRServerDefaultsRuntime(t *testing.T) {
	if got := NewSSRServer(t.TempDir(), 0, "").runtime; got != "node" {
		t.Errorf("default runtime = %q, want node", got)
	}
	if got := NewSSRServer(t.TempDir(), 0, "deno").runtime; got != "deno" {
		t.Errorf("runtime = %q, want deno", got)
	}
}
