package app

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSingleInstance(t *testing.T) {
	dir := t.TempDir()
	got := make(chan SecondInstance, 2)
	first, ok, err := Acquire(dir, "com.acme.app", []string{"a"}, func(si SecondInstance) { got <- si })
	if err != nil || !ok {
		t.Fatalf("first = %v, %v", ok, err)
	}
	defer first.Release()
	inst, ok, err := Acquire(dir, "com.acme.app", []string{"--x", "acme://open/1"}, nil)
	if err != nil || ok || inst != nil {
		t.Fatalf("second = %v, %v, %v", inst, ok, err)
	}
	select {
	case si := <-got:
		if !reflect.DeepEqual(si.Args, []string{"--x", "acme://open/1"}) || !reflect.DeepEqual(si.URLs, []string{"acme://open/1"}) || si.Dir == "" {
			t.Fatalf("second instance event %+v", si)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the first instance heard nothing")
	}
	// Another app id is independent.
	other, ok, err := Acquire(dir, "com.other.app", nil, nil)
	if err != nil || !ok {
		t.Fatalf("other app: %v %v", ok, err)
	}
	other.Release()
	// After release (or a crash that leaves the file) a new instance is first.
	first.Release()
	again, ok, err := Acquire(dir, "com.acme.app", nil, nil)
	if err != nil || !ok {
		t.Fatalf("after release: %v %v", ok, err)
	}
	again.Release()
}

// When the runtime directory is too long for a socket path, the lock socket
// goes to a private directory in the home directory, not to the shared
// temporary directory, where another user could create the path first and
// receive the launch arguments of every later start.
func TestLongPathAvoidsTheSharedTempDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix socket paths")
	}
	home, err := os.MkdirTemp("", "h")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	t.Setenv("HOME", home)
	long := filepath.Join(t.TempDir(), strings.Repeat("d", 120))
	p, err := socketPath(long, "com.acme.app")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(p, home+string(filepath.Separator)) {
		t.Fatalf("socket %q is not under the home directory %q", p, home)
	}
	st, err := os.Stat(filepath.Dir(p))
	if err != nil || st.Mode().Perm() != 0o700 {
		t.Fatalf("socket directory: %v %v", st.Mode(), err)
	}
}
