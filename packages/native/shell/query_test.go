package shell

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/platform"
)

// scriptWindow plays the page: it answers the query script the way the real
// page would, by calling the dev endpoint.
type scriptWindow struct {
	platform.Window
	mu      sync.Mutex
	evals   int
	ignore  int    // number of evals to ignore (page still loading)
	result  string // JSON body of the answer
	unload  bool   // unload instead of answering
	handler http.Handler
}

var idRe = regexp.MustCompile(`id=(\d+)&phase=start`)

func (w *scriptWindow) Eval(js string) {
	w.mu.Lock()
	w.evals++
	n := w.evals
	w.mu.Unlock()
	if n <= w.ignore {
		return
	}
	m := idRe.FindStringSubmatch(js)
	if m == nil {
		return
	}
	post := func(suffix string, body string) {
		req := httptest.NewRequest(http.MethodPost, queryPath+"?id="+m[1]+suffix, strings.NewReader(body))
		w.handler.ServeHTTP(httptest.NewRecorder(), req)
	}
	go func() {
		post("&phase=start", "1")
		if w.unload {
			post("&phase=gone", "1")
			return
		}
		post("", w.result)
	}()
}

func newQueryControl(w *scriptWindow) *Control {
	a := &app{opts: RunOptions{Dev: true}}
	w.handler = http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if !a.results.serve(rw, r) {
			http.NotFound(rw, r)
		}
	})
	return &Control{a: a, wins: &windows{list: []platform.Window{w}}}
}

func TestQueryReturnsTheValue(t *testing.T) {
	w := &scriptWindow{result: `{"ok":true,"v":{"n":3}}`}
	got, err := newQueryControl(w).Query("1+2")
	if err != nil || got != `{"n":3}` {
		t.Fatalf("Query = %q, %v", got, err)
	}
}

func TestQueryReportsPageErrors(t *testing.T) {
	w := &scriptWindow{result: `{"ok":false,"error":"ReferenceError: x is not defined"}`}
	if _, err := newQueryControl(w).Query("x"); err == nil || !strings.Contains(err.Error(), "ReferenceError") {
		t.Fatalf("err = %v", err)
	}
}

func TestQueryResendsUntilThePageRunsIt(t *testing.T) {
	old := startWait
	startWait = 20 * time.Millisecond
	defer func() { startWait = old }()
	w := &scriptWindow{ignore: 2, result: `{"ok":true,"v":"late"}`}
	got, err := newQueryControl(w).Query("1")
	if err != nil || got != `"late"` {
		t.Fatalf("Query = %q, %v", got, err)
	}
	if w.evals != 3 {
		t.Errorf("evals = %d, want 3", w.evals)
	}
}

func TestQueryFailsAtOnceWhenThePageUnloads(t *testing.T) {
	w := &scriptWindow{unload: true}
	start := time.Now()
	_, err := newQueryControl(w).Query("1")
	if err == nil || !strings.Contains(err.Error(), "unloaded") {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Error("an unloaded page must not wait for the timeout")
	}
}

func TestQueryNeedsDev(t *testing.T) {
	c := &Control{a: &app{}}
	if _, err := c.Query("1"); err == nil || !strings.Contains(err.Error(), "--dev") {
		t.Fatalf("err = %v", err)
	}
}

type queryCtl struct {
	fakeCtl
	value string
}

func (q *queryCtl) Query(js string) (string, error) { return q.value + js, nil }

func TestServeControlQuery(t *testing.T) {
	var out strings.Builder
	ServeControl(strings.NewReader("query 1+1\nquery\n"), &out, &queryCtl{value: "v:"})
	if got := out.String(); got != "ok v:1+1\nerror query needs a JavaScript expression\n" {
		t.Errorf("replies = %q", got)
	}
	out.Reset()
	ServeControl(strings.NewReader("query 1\n"), &out, &fakeCtl{})
	if got := out.String(); got != "error query is not supported here\n" {
		t.Errorf("replies = %q", got)
	}
}
