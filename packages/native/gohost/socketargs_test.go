package gohost

import (
	"strings"
	"testing"
)

func TestExpandSocketArgs(t *testing.T) {
	in := []string{"run", "--allow-net=127.0.0.1,unix:{socket}", "--allow-read=/x", "{socket}.log"}
	got := expandSocketArgs(in, "/run/g001.sock")
	want := []string{"run", "--allow-net=127.0.0.1,unix:/run/g001.sock", "--allow-read=/x", "/run/g001.sock.log"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("unix: %v", got)
	}
	// The TCP transport has no socket: the unix entry disappears.
	got = expandSocketArgs(in, "")
	if got[1] != "--allow-net=127.0.0.1" {
		t.Errorf("tcp: %v", got)
	}
	if in[1] != "--allow-net=127.0.0.1,unix:{socket}" {
		t.Error("the input was modified")
	}
	if got := expandSocketArgs(nil, "x"); got != nil {
		t.Errorf("nil args: %v", got)
	}
}
