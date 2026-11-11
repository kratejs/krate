package windows

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseStreamMsg(t *testing.T) {
	good := `{"k":"open","id":3,"p":"/__krate/signals?names=a","h":[["x-krate-window","w1"]]}`
	m, err := parseStreamMsg(good)
	if err != nil || m.ID != 3 || m.P != "/__krate/signals?names=a" || len(m.H) != 1 {
		t.Fatalf("%+v %v", m, err)
	}
	if m, err := parseStreamMsg(`{"k":"cancel","id":3}`); err != nil || m.K != "cancel" {
		t.Fatalf("%+v %v", m, err)
	}
	for _, bad := range []string{
		``, `x`, `{"k":"open","id":0,"p":"/a"}`, `{"k":"open","id":1,"p":"//evil.example/a"}`,
		`{"k":"open","id":1,"p":"https://evil.example/a"}`, `{"k":"open","id":1,"p":"/a\\b"}`,
		`{"k":"other","id":1}`, `{"k":"open","id":1,"p":""}`,
	} {
		if _, err := parseStreamMsg(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if _, err := parseStreamMsg(strings.Repeat("x", maxStreamMsg+1)); err == nil {
		t.Error("accepted an oversized message")
	}
}

func TestStreamTable(t *testing.T) {
	var tab streamTable
	cancelled := map[int]bool{}
	mk := func(id int) context.CancelFunc { return func() { cancelled[id] = true } }
	if !tab.add(1, mk(1)) || tab.add(1, mk(1)) {
		t.Fatal("duplicate id accepted")
	}
	tab.cancel(1)
	if !cancelled[1] {
		t.Fatal("cancel did not run")
	}
	tab.remove(1)
	for i := 10; i < 10+maxStreamsPerView; i++ {
		if !tab.add(i, mk(i)) {
			t.Fatalf("add %d refused", i)
		}
	}
	if tab.add(999, mk(999)) {
		t.Fatal("limit not enforced")
	}
	tab.closeAll()
	if !cancelled[10] || tab.add(1, mk(1)) {
		t.Fatal("closeAll must cancel and refuse")
	}
	for i := 10; i < 10+maxStreamsPerView; i++ {
		tab.remove(i) // the stream goroutines do this as their handlers return
	}
	tab.reopen()
	if !tab.add(1, mk(1)) {
		t.Fatal("reopen did not accept streams")
	}
}

type sink struct {
	mu   sync.Mutex
	msgs []map[string]any
}

func (s *sink) send(m string) {
	var v map[string]any
	_ = json.Unmarshal([]byte(m), &v)
	s.mu.Lock()
	s.msgs = append(s.msgs, v)
	s.mu.Unlock()
}

func (s *sink) all() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.msgs...)
}

func TestRunStreamSendsHeadDataEnd(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/s" || r.Header.Get("X-A") != "b" || r.Method != http.MethodGet {
			t.Errorf("request: %s %s %v", r.Method, r.URL, r.Header)
		}
		w.Header().Set("Content-Type", "application/x-krate-frames")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello"))
		w.(http.Flusher).Flush()
		_, _ = w.Write(make([]byte, dataChunk+10))
	})
	var s sink
	runStream(context.Background(), h, 5, "/s", [][2]string{{"X-A", "b"}}, s.send)
	msgs := s.all()
	if len(msgs) != 5 {
		t.Fatalf("%d messages: %v", len(msgs), msgs)
	}
	if msgs[0]["k"] != "head" || msgs[0]["s"] != float64(200) || msgs[0]["id"] != float64(5) {
		t.Errorf("head: %v", msgs[0])
	}
	hdrs, _ := msgs[0]["h"].([]any)
	if len(hdrs) != 1 {
		t.Errorf("headers (connection must be dropped): %v", hdrs)
	}
	b, _ := base64.StdEncoding.DecodeString(msgs[1]["d"].(string))
	if string(b) != "hello" || msgs[2]["k"] != "data" || msgs[3]["k"] != "data" || msgs[4]["k"] != "end" {
		t.Errorf("messages: %v", msgs)
	}
}

func TestRunStreamCancelStopsHandlerQuietly(t *testing.T) {
	started := make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("first"))
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	var s sink
	done := make(chan struct{})
	go func() { runStream(ctx, h, 1, "/s", nil, s.send); close(done) }()
	<-started
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler was not stopped")
	}
	for _, m := range s.all() {
		if m["k"] == "end" || m["k"] == "err" {
			t.Errorf("a cancelled stream must say nothing more: %v", m)
		}
	}
}

func TestRunStreamPanicBecomesError(t *testing.T) {
	var s sink
	runStream(context.Background(), http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("x") }), 2, "/s", nil, s.send)
	msgs := s.all()
	if len(msgs) != 1 || msgs[0]["k"] != "err" {
		t.Fatalf("%v", msgs)
	}
}

func TestWholeResponseIsNotCommittedEarly(t *testing.T) {
	committed := false
	var got []byte
	w := newResponseWriter(false,
		func(int, http.Header, *blockBuf) { committed = true },
		func(_ int, _ http.Header, b []byte) { got = b })
	w.whole = true
	_, _ = w.Write(make([]byte, commitThreshold*3))
	w.Flush()
	w.finish()
	if committed || len(got) != commitThreshold*3 {
		t.Fatalf("committed=%v len=%d", committed, len(got))
	}
}

// The injected script is exercised in node against a fake WebView2 message
// channel: a streamed GET comes back as a Response whose body yields the
// chunks, an abort sends a cancel, and other requests use the real fetch.
func TestStreamShimInNode(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("KRATE_REQUIRE_E2E") == "1" {
			t.Fatal("node is required")
		}
		t.Skip("node not found")
	}
	dir := t.TempDir()
	harness := `
const posted = [];
const listeners = [];
globalThis.window = globalThis;
globalThis.location = new URL("` + Origin + `/index.html");
globalThis.addEventListener = (n, f) => { if (n === "pagehide") globalThis.__pagehide = f; };
globalThis.chrome = { webview: {
  postMessage(m) { posted.push(JSON.parse(m)); },
  addEventListener(n, f) { listeners.push(f); },
}};
let realCalls = 0;
globalThis.fetch = async () => { realCalls++; return new Response("real"); };
const deliver = (o) => listeners.forEach((f) => f({ data: JSON.stringify(o) }));
const b64 = (s) => Buffer.from(s).toString("base64");
const assert = (c, m) => { if (!c) { console.error("FAIL " + m); process.exit(1); } };
eval(process.env.SHIM);
(async () => {
  assert(String(await (await fetch("/plain")).text()) === "real" && realCalls === 1, "plain goes to fetch");
  assert((await (await fetch("/__krate/signals?names=a", { method: "POST" })).text()) === "real", "POST goes to fetch");
  const p = fetch("/__krate/signals?names=a", { headers: { "x-krate-window": "w1" } });
  assert(posted.length === 1 && posted[0].k === "open" && posted[0].p === "/__krate/signals?names=a", "open posted");
  assert(JSON.stringify(posted[0].h) === '[["x-krate-window","w1"]]', "headers posted " + JSON.stringify(posted[0].h));
  const id = posted[0].id;
  deliver({ k: "head", id, s: 200, h: [["content-type", "text/plain"]] });
  const res = await p;
  assert(res.status === 200 && res.headers.get("content-type") === "text/plain", "head");
  const rd = res.body.getReader();
  deliver({ k: "data", id, d: b64("one") });
  let r = await rd.read();
  assert(new TextDecoder().decode(r.value) === "one", "chunk one");
  deliver({ k: "data", id, d: b64("two") });
  deliver({ k: "end", id });
  r = await rd.read();
  assert(new TextDecoder().decode(r.value) === "two", "chunk two");
  r = await rd.read();
  assert(r.done, "end closes the body");

  const ac = new AbortController();
  const q = fetch("/__krate/stream/s7", { signal: ac.signal });
  const id2 = posted[posted.length - 1].id;
  ac.abort();
  let name = "";
  try { await q; } catch (e) { name = e.name; }
  assert(name === "AbortError", "abort before the head rejects: " + name);
  assert(posted[posted.length - 1].k === "cancel" && posted[posted.length - 1].id === id2, "cancel posted");

  const q2 = fetch("/__krate/hmr");
  const id3 = posted[posted.length - 1].id;
  deliver({ k: "head", id: id3, s: 200, h: [] });
  const res2 = await q2;
  const rd2 = res2.body.getReader();
  await rd2.cancel();
  assert(posted[posted.length - 1].k === "cancel" && posted[posted.length - 1].id === id3, "reader cancel posts cancel");

  const q3 = fetch("/__krate/signals?names=b");
  const id4 = posted[posted.length - 1].id;
  deliver({ k: "err", id: id4, e: "boom" });
  let msg = "";
  try { await q3; } catch (e) { msg = e.message; }
  assert(msg === "boom", "error before the head rejects");

  const q4 = fetch("/__krate/signals?names=c");
  const n = posted.length;
  globalThis.__pagehide();
  assert(posted.length === n + 1 && posted[n].k === "cancel", "pagehide cancels open streams");
  console.log("ok");
})().catch((e) => { console.error("FAIL", e); process.exit(1); });
`
	f := filepath.Join(dir, "h.js")
	if err := os.WriteFile(f, []byte(harness), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, f)
	cmd.Env = append(os.Environ(), "SHIM="+streamShim)
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "ok") {
		t.Fatalf("node: %v\n%s", err, out)
	}
}
