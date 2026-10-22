package build

import (
	"net"
	"testing"
)

func TestInternalPortFallsBackWhenTaken(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	taken := ln.Addr().(*net.TCPAddr).Port
	if got := internalPort(taken); got == taken || got == 0 {
		t.Fatalf("internalPort(%d) = %d, want a different free port", taken, got)
	}
	free, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := free.Addr().(*net.TCPAddr).Port
	free.Close()
	if got := internalPort(port); got != port {
		t.Fatalf("internalPort(%d) = %d, want the preferred port when free", port, got)
	}
}
