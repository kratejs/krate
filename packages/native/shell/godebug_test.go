package shell

import (
	"net"
	"net/rpc"
	"net/rpc/jsonrpc"
	"reflect"
	"testing"
	"time"
)

func TestDlvArgs(t *testing.T) {
	got := dlvArgs("127.0.0.1:2345", "/app/krate-gohost", nil)
	want := []string{"exec", "--headless", "--listen=127.0.0.1:2345", "--api-version=2", "--accept-multiclient", "--continue", "--log=false", "/app/krate-gohost"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args %q", got)
	}
	if got := dlvArgs("a:1", "b", []string{"x", "y"}); !reflect.DeepEqual(got[len(got)-3:], []string{"--", "x", "y"}) {
		t.Fatalf("program args %q", got)
	}
}

type fakeDelve struct{ detached chan bool }

func (f *fakeDelve) Detach(in struct{ Kill bool }, out *struct{}) error {
	f.detached <- in.Kill
	return nil
}

func TestDlvDetachKillsTheProgram(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	f := &fakeDelve{detached: make(chan bool, 1)}
	srv := rpc.NewServer()
	if err := srv.RegisterName("RPCServer", f); err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go srv.ServeCodec(jsonrpc.NewServerCodec(c))
		}
	}()
	_, stop := dlvWrap("", ln.Addr().String())
	stop()
	select {
	case kill := <-f.detached:
		if !kill {
			t.Fatal("Detach must kill the program")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no Detach call")
	}
	// Nobody listening: stop returns quietly.
	ln.Close()
	stop()
}
