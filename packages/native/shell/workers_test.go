package shell

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/hostchild"
	"github.com/kratejs/krate/packages/native/internal/gohosttest"
	"github.com/kratejs/krate/packages/native/sdk/port"
	"github.com/kratejs/krate/packages/native/sdk/worker"
)

const nodeWorkerJS = `import { start } from "./hostchild.mjs";
const h = globalThis.__krateHost;
const emit = (v) => h.call("krate/worker.emit", [v]);
h.register("__worker.message", async (m) => {
  if (m && m.op === "echo") await emit(m);
  else if (m && m.op === "die") process.exit(3);
  else if (m && m.op === "bin") await emit(new Uint8Array([m.n, 2, 3]));
  else if (m && m.op === "kv") {
    try { await h.call("krate/kv.get", ["x"]); await emit({ kv: "ok" }); }
    catch (e) { await emit({ kv: e.code }); }
  } else if (m && m.op === "listen") {
    (async () => {
      for await (const ev of h.stream("krate/port.listen", [m.port])) {
        if (ev.closed) { await emit({ closed: true }); return; }
        await h.call("krate/port.post", [m.port, { reply: ev.data }]);
      }
    })();
  }
  return null;
});
console.log("hello from the worker");
start();
`

func workerApp(t *testing.T, root string, workers []map[string]any, perms []string) string {
	t.Helper()
	app, err := buildTestApp(root, "<!doctype html><title>idx</title>hello", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	writeAll := func(name, s string) {
		if err := writeFile(filepath.Join(app.dir, filepath.FromSlash(name)), s); err != nil {
			t.Fatal(err)
		}
	}
	writeAll("workers/hostchild.mjs", hostchild.Source)
	writeAll("workers/echo.mjs", nodeWorkerJS)
	raw, err := os.ReadFile(filepath.Join(app.dir, ManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	m["workers"] = workers
	m["permissions"] = perms
	b, _ := json.Marshal(m)
	writeAll(ManifestFile, string(b))
	return app.dir
}

func nodeWorker(name string, restart string, perms ...string) map[string]any {
	return map[string]any{
		"name": name, "runtime": "node", "entry": "workers/echo.mjs", "command": []string{"node", "{entry}"},
		"permissions": perms, "restart": map[string]any{"policy": restart, "backoffMs": 20, "max": 3},
	}
}

type notices chan worker.Notice

func watch(t *testing.T, id string) (notices, func()) {
	t.Helper()
	ch := make(notices, 128)
	cancel, err := worker.OnEvent(id, func(n worker.Notice) { ch <- n })
	if err != nil {
		t.Fatal(err)
	}
	return ch, cancel
}

func (c notices) wait(t *testing.T, kind string) worker.Notice {
	t.Helper()
	deadline := time.After(30 * time.Second)
	for {
		select {
		case n := <-c:
			if n.Kind == kind {
				return n
			}
		case <-deadline:
			t.Fatalf("no %q event", kind)
		}
	}
}

func needNode(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		if os.Getenv("KRATE_REQUIRE_E2E") == "1" {
			t.Fatal("node is not on PATH")
		}
		t.Skip("node is not on PATH")
	}
}

func TestWorkersNode(t *testing.T) {
	needNode(t)
	root, cleanup, err := gohosttest.TempDir("shell-wk-")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	dir := workerApp(t, root, []map[string]any{
		nodeWorker("plain", "never"),
		nodeWorker("keeper", "onFailure"),
		nodeWorker("kv", "never", "kv"),
		nodeWorker("ported", "never", "port"),
	}, []string{"worker", "kv", "port"})
	ctl, stop := runBrowser(t, dir, newReportSink())
	defer stop()
	_ = ctl

	t.Run("specs and errors", func(t *testing.T) {
		specs, err := worker.Specs()
		if err != nil || len(specs) != 4 {
			t.Fatalf("specs %+v %v", specs, err)
		}
		if _, err := worker.Start("nope", nil); err == nil || !strings.Contains(err.Error(), "no worker") {
			t.Fatalf("starting an undeclared worker: %v", err)
		}
	})

	t.Run("messages both ways with binary", func(t *testing.T) {
		id, err := worker.Start("plain", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = worker.Stop(id) }()
		ev, cancel := watch(t, id)
		defer cancel()
		if err := worker.Post(id, map[string]any{"op": "echo", "n": 7}); err != nil {
			t.Fatal(err)
		}
		n := ev.wait(t, worker.EventMessage)
		if n.Message == nil || string(n.Message.Data) != `{"n":7,"op":"echo"}` {
			t.Fatalf("echo: %+v", n)
		}
		if err := worker.Post(id, map[string]any{"op": "bin", "n": 9}); err != nil {
			t.Fatal(err)
		}
		n = ev.wait(t, worker.EventMessage)
		if n.Message == nil || string(n.Message.Bytes) != "\x09\x02\x03" {
			t.Fatalf("binary: %+v", n.Message)
		}
		if err := worker.PostBytes(id, []byte{1}); err != nil {
			t.Fatal(err)
		}
		list, _ := worker.List()
		if len(list) == 0 || list[0].Pid == 0 {
			t.Fatalf("list: %+v", list)
		}
	})

	t.Run("crash restarts per policy and stop is final", func(t *testing.T) {
		id, err := worker.Start("keeper", nil)
		if err != nil {
			t.Fatal(err)
		}
		ev, cancel := watch(t, id)
		defer cancel()
		_ = worker.Post(id, map[string]any{"op": "die"})
		c := ev.wait(t, worker.EventCrash)
		if c.ExitCode == nil || *c.ExitCode != 3 {
			t.Fatalf("crash: %+v", c)
		}
		ev.wait(t, worker.EventRestarting)
		ev.wait(t, worker.EventStarted)
		// The new process answers.
		_ = worker.Post(id, map[string]any{"op": "echo", "n": 1})
		ev.wait(t, worker.EventMessage)
		if err := worker.Stop(id); err != nil {
			t.Fatal(err)
		}
		ev.wait(t, worker.EventStopped)
		if err := worker.Post(id, 1); err == nil {
			t.Fatal("posting to a stopped worker succeeded")
		}
	})

	t.Run("a worker with no permissions cannot call the SDK", func(t *testing.T) {
		for name, want := range map[string]string{"plain": `"permission_denied"`, "kv": `"ok"`} {
			id, err := worker.Start(name, nil)
			if err != nil {
				t.Fatal(err)
			}
			ev, cancel := watch(t, id)
			_ = worker.Post(id, map[string]any{"op": "kv"})
			n := ev.wait(t, worker.EventMessage)
			cancel()
			_ = worker.Stop(id)
			if n.Message == nil || !strings.Contains(string(n.Message.Data), want) {
				t.Fatalf("%s: %+v", name, n.Message)
			}
		}
	})

	t.Run("host to worker through a port", func(t *testing.T) {
		id, err := worker.Start("ported", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = worker.Stop(id) }()
		ends, err := port.Channel()
		if err != nil {
			t.Fatal(err)
		}
		if err := port.Transfer(ends.B, port.WorkerOwner(id)); err != nil {
			t.Fatal(err)
		}
		got := make(chan port.Message, 4)
		closed := make(chan struct{})
		cancel, err := port.Listen(ends.A, func(m port.Message) { got <- m }, func() { close(closed) })
		if err != nil {
			t.Fatal(err)
		}
		defer cancel()
		if err := worker.Post(id, map[string]any{"op": "listen", "port": ends.B}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(300 * time.Millisecond)
		if err := port.Post(ends.A, "ping"); err != nil {
			t.Fatal(err)
		}
		select {
		case m := <-got:
			if string(m.Data) != `{"reply":"ping"}` {
				t.Fatalf("reply %s", m.Data)
			}
		case <-time.After(20 * time.Second):
			t.Fatal("no reply through the port")
		}
		// Stopping the worker closes the channel.
		_ = worker.Stop(id)
		select {
		case <-closed:
		case <-time.After(10 * time.Second):
			t.Fatal("the port stayed open after the worker stopped")
		}
	})

	t.Run("a worker without port:receive is not handed a port", func(t *testing.T) {
		id, err := worker.Start("plain", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = worker.Stop(id) }()
		ends, _ := port.Channel()
		err = port.Transfer(ends.B, port.WorkerOwner(id))
		if err == nil || !strings.Contains(err.Error(), "port:receive") {
			t.Fatalf("transfer to a worker without the permission: %v", err)
		}
	})
}

func TestParseManifestWorkers(t *testing.T) {
	base := `{"appId":"com.acme.app","permissions":["worker","kv"],"workers":[%s]}`
	ok := `{"name":"a","runtime":"node","entry":"w/a.mjs","permissions":["kv"],"restart":{"policy":"onFailure"}}`
	m, err := ParseManifest([]byte(strings.Replace(base, "%s", ok, 1)), "app.json")
	if err != nil || len(m.Workers) != 1 || m.Workers[0].Restart.Policy != "onFailure" {
		t.Fatalf("%+v %v", m, err)
	}
	for name, bad := range map[string]string{
		"bad name":      `{"name":"a b","runtime":"node","entry":"x"}`,
		"escape":        `{"name":"a","runtime":"node","entry":"../x"}`,
		"unknown perm":  `{"name":"a","runtime":"node","entry":"x","permissions":["nope"]}`,
		"not granted":   `{"name":"a","runtime":"node","entry":"x","permissions":["fs"]}`,
		"bad policy":    `{"name":"a","runtime":"node","entry":"x","restart":{"policy":"sometimes"}}`,
		"duplicate":     `{"name":"a","runtime":"node","entry":"x"},{"name":"a","runtime":"node","entry":"y"}`,
		"unknown kind":  `{"name":"a","runtime":"python","entry":"x"}`,
		"missing entry": `{"name":"a","runtime":"node"}`,
	} {
		if _, err := ParseManifest([]byte(strings.Replace(base, "%s", bad, 1)), "app.json"); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

const goWorkerSrc = `package main

import (
	"fmt"
	"io"
	"sync"
	"github.com/kratejs/krate/packages/native/sdk/worker"
)

func main() {
	worker.Serve(func(m worker.Message) {
		if m.Bytes != nil {
			_ = worker.EmitBytes(append([]byte("got:"), m.Bytes...))
			return
		}
		_ = worker.Emit(map[string]any{"echo": m.Data})
	})
}
`

func TestWorkerGo(t *testing.T) {
	root, cleanup, err := gohosttest.TempDir("shell-wkgo-")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	app, err := buildTestApp(root, "<!doctype html><title>idx</title>hello", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	bin, err := gohosttest.Build(root, "gworker", goWorkerSrc)
	if err != nil {
		t.Skipf("cannot build a Go worker here: %v", err)
	}
	rel := "workers/gworker"
	if err := os.MkdirAll(filepath.Join(app.dir, "workers"), 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(bin)
	if err := os.WriteFile(filepath.Join(app.dir, filepath.FromSlash(rel)+exeSuffix()), data, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(app.dir, ManifestFile))
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	m["permissions"] = []string{"worker"}
	m["workers"] = []map[string]any{{"name": "g", "runtime": "go", "entry": rel}}
	b, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(app.dir, ManifestFile), b, 0o644); err != nil {
		t.Fatal(err)
	}
	_, stop := runBrowser(t, app.dir, newReportSink())
	defer stop()
	id, err := worker.Start("g", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = worker.Stop(id) }()
	ev, cancel := watch(t, id)
	defer cancel()
	_ = worker.Post(id, map[string]any{"a": 1})
	n := ev.wait(t, worker.EventMessage)
	if n.Message == nil || string(n.Message.Data) != `{"echo":{"a":1}}` {
		t.Fatalf("%+v", n.Message)
	}
	_ = worker.PostBytes(id, []byte("xyz"))
	n = ev.wait(t, worker.EventMessage)
	if n.Message == nil || string(n.Message.Bytes) != "got:xyz" {
		t.Fatalf("%+v", n.Message)
	}
}

func exeSuffix() string {
	if strings.HasSuffix(os.Args[0], ".exe") {
		return ".exe"
	}
	return ""
}

// A worker that cannot start must say why in the shell log and fail the call
// that started it, rather than leave the page waiting for messages.
func TestWorkerStartFailureIsLogged(t *testing.T) {
	needNode(t)
	root, cleanup, err := gohosttest.TempDir("shell-wkfail-")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	missing := nodeWorker("missing", "never")
	missing["entry"] = "workers/absent.mjs"
	dies := nodeWorker("dies", "never")
	dies["entry"] = "workers/dies.mjs"
	dir := workerApp(t, root, []map[string]any{missing, dies}, []string{"worker"})
	src := "console.error('worker boom: bad config');\nprocess.exit(2);\n"
	if err := os.WriteFile(filepath.Join(dir, "workers", "dies.mjs"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var logs []string
	ready := make(chan *Control, 1)
	done := make(chan error, 1)
	go func() {
		done <- Run(dir, RunOptions{
			Browser: true, Dev: true, Out: io.Discard, ExtraHandler: newReportSink().handle,
			Logf: func(format string, args ...any) {
				mu.Lock()
				logs = append(logs, fmt.Sprintf(format, args...))
				mu.Unlock()
			},
			OnReady: func(c *Control) { ready <- c },
		})
	}()
	select {
	case ctl := <-ready:
		defer func() {
			ctl.Quit()
			<-done
		}()
	case err := <-done:
		t.Fatalf("Run returned early: %v", err)
	case <-time.After(60 * time.Second):
		t.Fatal("not ready")
	}
	logged := func() string {
		mu.Lock()
		defer mu.Unlock()
		return strings.Join(logs, "\n")
	}

	for name, want := range map[string]string{"missing": "absent.mjs", "dies": "worker boom: bad config"} {
		_, err := worker.Start(name, nil)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("starting %s: %v, want an error naming %q", name, err, want)
		}
		if l := logged(); !strings.Contains(l, "the "+name+" worker failed to start") || !strings.Contains(l, want) {
			t.Fatalf("the shell log lacks the %s failure:\n%s", name, l)
		}
	}
	if list, _ := worker.List(); len(list) != 0 {
		t.Fatalf("failed starts left instances: %+v", list)
	}
}
