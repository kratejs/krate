package shell

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/internal/gohosttest"
	"github.com/kratejs/krate/packages/native/scheme"
)

// readJSONFrame reads one frame and returns its JSON part (no blobs expected).
func readJSONFrame(r io.Reader) (map[string]json.RawMessage, error) {
	var n [4]byte
	if _, err := io.ReadFull(r, n[:]); err != nil {
		return nil, err
	}
	body := make([]byte, binary.BigEndian.Uint32(n[:]))
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	var k [2]byte
	if _, err := io.ReadFull(r, k[:]); err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	return m, json.Unmarshal(body, &m)
}

type frameResult struct {
	m   map[string]json.RawMessage
	err error
}

// frameReader pumps frames from body so tests can wait with a timeout.
func frameReader(body io.Reader) <-chan frameResult {
	ch := make(chan frameResult, 16)
	go func() {
		br := bufio.NewReader(body)
		for {
			m, err := readJSONFrame(br)
			ch <- frameResult{m, err}
			if err != nil {
				return
			}
		}
	}()
	return ch
}

func nextHMR(t *testing.T, ch <-chan frameResult) map[string]any {
	t.Helper()
	select {
	case fr := <-ch:
		if fr.err != nil {
			t.Fatalf("reading hmr frame: %v", fr.err)
		}
		var msg map[string]any
		if err := json.Unmarshal(fr.m["hmr"], &msg); err != nil {
			t.Fatalf("hmr frame %q: %v", fr.m, err)
		}
		return msg
	case <-time.After(10 * time.Second):
		t.Fatal("no hmr frame")
	}
	return nil
}

func TestHMRHubPublish(t *testing.T) {
	h := newHMRHub()
	if err := h.publish([]byte(`not json`)); err == nil {
		t.Error("garbage accepted")
	}
	if err := h.publish([]byte(`[1]`)); err == nil {
		t.Error("array accepted")
	}
	ch, replay, cancel := h.subscribe()
	defer cancel()
	if replay != nil {
		t.Error("replay before any error")
	}
	if err := h.publish([]byte(` {"type": "build-error", "diagnostics": []} `)); err != nil {
		t.Fatal(err)
	}
	fr := <-ch
	if !bytes.Contains(fr, []byte(`{"hmr":{"type":"build-error","diagnostics":[]}}`)) {
		t.Errorf("frame = %q", fr)
	}
	_, replay, cancel2 := h.subscribe()
	defer cancel2()
	if !bytes.Equal(replay, fr) {
		t.Errorf("late subscriber replay = %q", replay)
	}
	if err := h.publish([]byte(`{"type":"reload"}`)); err != nil {
		t.Fatal(err)
	}
	_, replay, cancel3 := h.subscribe()
	defer cancel3()
	if replay != nil {
		t.Error("error replayed after a good reload")
	}
}

func TestEventRing(t *testing.T) {
	r := newEventRing(3)
	for i := 0; i < 5; i++ {
		r.add(scheme.Event{Kind: "call", Fn: "f"})
	}
	got := r.since(0)
	if len(got) != 3 || got[0].Seq != 3 || got[2].Seq != 5 {
		t.Fatalf("ring = %+v", got)
	}
	if got := r.since(4); len(got) != 1 || got[0].Seq != 5 {
		t.Fatalf("since 4 = %+v", got)
	}
}

func TestDevEndpointsBrowserMode(t *testing.T) {
	root, cleanup, err := gohosttest.TempDir("shell-")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	app, err := buildTestApp(root, "<!doctype html><title>idx</title>hello", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	ctl, _ := runBrowser(t, app.dir, newReportSink())
	u, _ := url.Parse(ctl.URL())
	base := "http://" + u.Host
	jar := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	tok := u.Query().Get("krate_token")
	withTok := func(r *http.Request) { r.Header.Set("X-Krate-Token", tok) }

	// The stream needs the token like everything else.
	if resp, _ := get(t, jar, base+"/__krate/hmr", nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("hmr without token: %d", resp.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodGet, base+"/__krate/hmr", nil)
	withTok(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/x-krate-frames" {
		t.Fatalf("hmr: %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	frames := frameReader(resp.Body)
	if m := nextHMR(t, frames); m["type"] != "connected" {
		t.Fatalf("first message = %v", m)
	}

	if err := ctl.HMR(`{"type":"reload","pages":["/"]}`); err != nil {
		t.Fatal(err)
	}
	if m := nextHMR(t, frames); m["type"] != "reload" {
		t.Fatalf("message = %v", m)
	}
	if err := ctl.HMR(`nope`); err == nil {
		t.Error("bad message accepted")
	}

	// Host traffic shows up in the events endpoint: a call that works and one
	// that does not exist.
	post := func(fn string) {
		req, _ := http.NewRequest(http.MethodPost, base+"/__krate/call", bytes.NewReader(frameReq(fn, nil)))
		withTok(req)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	post("h.tag")
	post("h.missing")
	_, body := get(t, jar, base+"/__krate/dev/events", withTok)
	var evs []loggedEvent
	br := bufio.NewReader(strings.NewReader(body))
	for {
		m, err := readJSONFrame(br)
		if err != nil {
			break
		}
		var ev loggedEvent
		if err := json.Unmarshal(m["ev"], &ev); err != nil {
			t.Fatal(err)
		}
		evs = append(evs, ev)
	}
	var okCalls, missing int
	for _, ev := range evs {
		if ev.Kind != "call" {
			continue
		}
		switch {
		case ev.Fn == "h.tag" && ev.Code == "":
			okCalls++
		case ev.Fn == "h.missing" && ev.Code == "not_found":
			missing++
		}
	}
	if okCalls < 1 || missing != 1 {
		t.Errorf("events: ok=%d missing=%d of %+v", okCalls, missing, evs)
	}

	// Client errors are accepted (and logged).
	req, _ = http.NewRequest(http.MethodPost, base+"/__krate/dev/client-error", strings.NewReader(`{"message":"boom","url":"x.js","line":3,"col":4}`))
	withTok(req)
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusNoContent {
		t.Errorf("client-error: %d", resp2.StatusCode)
	}
}

func TestServeControlHMR(t *testing.T) {
	f := &fakeCtl{}
	var out bytes.Buffer
	ServeControl(strings.NewReader("hmr {\"type\":\"reload\"}\nhmr\n"), &out, f)
	if out.String() != "ok\nerror hmr needs a JSON message\n" || strings.Join(f.calls, ";") != `hmr {"type":"reload"}` {
		t.Errorf("out=%q calls=%q", out.String(), f.calls)
	}
}
