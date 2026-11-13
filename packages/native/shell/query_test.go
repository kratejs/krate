package shell

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
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

// navigatingPage plays a single view for query(..., single=true): it starts
// the query in document "a", never answers, and answers pings from doc.
func navigatingPage(c *Control, doc func() string, answerAfterPings int, answer string) func(string) error {
	var mu sync.Mutex
	pings := 0
	post := func(q, body string) {
		req := httptest.NewRequest(http.MethodPost, queryPath+"?"+q, strings.NewReader(body))
		rw := httptest.NewRecorder()
		c.a.results.serve(rw, req)
	}
	pingRe := regexp.MustCompile(`id=(\d+)&phase=ping`)
	return func(js string) error {
		if m := idRe.FindStringSubmatch(js); m != nil {
			go post("id="+m[1]+"&phase=start", "a")
			return nil
		}
		if m := pingRe.FindStringSubmatch(js); m != nil {
			mu.Lock()
			pings++
			n := pings
			mu.Unlock()
			go func() {
				post("id="+m[1]+"&phase=ping", doc())
				if answerAfterPings > 0 && n == answerAfterPings {
					post("id="+m[1], answer)
				}
			}()
		}
		return nil
	}
}

func TestQueryViewEndsWhenTheViewShowsAnotherDocument(t *testing.T) {
	old := pingEvery
	pingEvery = 20 * time.Millisecond
	defer func() { pingEvery = old }()
	c := newQueryControl(&scriptWindow{})
	start := time.Now()
	_, err := c.query(navigatingPage(c, func() string { return "b" }, 0, ""), "1", true)
	if err == nil || !strings.Contains(err.Error(), "unloaded") {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Error("a navigated view must not wait for the timeout")
	}
}

func TestQueryViewKeepsWaitingOnTheSameDocument(t *testing.T) {
	old := pingEvery
	pingEvery = 20 * time.Millisecond
	defer func() { pingEvery = old }()
	c := newQueryControl(&scriptWindow{})
	got, err := c.query(navigatingPage(c, func() string { return "a" }, 3, `{"ok":true,"v":"slow"}`), "1", true)
	if err != nil || got != `"slow"` {
		t.Fatalf("query = %q, %v", got, err)
	}
}

// TestQueryScriptRunsOncePerPage runs the real query script twice on one
// "page" in Node, as the shell does when the start report is slow: the
// expression must run once. When the start report fails (the page could not
// reach the endpoint yet), a resent copy runs it.
func TestQueryScriptRunsOncePerPage(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	run := func(failFirst bool) string {
		t.Helper()
		fail := "false"
		if failFirst {
			fail = "true"
		}
		js := `let calls = 0, failNext = ` + fail + `;
globalThis.addEventListener = () => {}; globalThis.removeEventListener = () => {};
globalThis.fetch = async (u) => {
  calls++;
  await new Promise((r) => setTimeout(r, 20));
  if (failNext && u.includes("phase=start")) { failNext = false; throw new TypeError("not reachable"); }
};
` + queryScript(7, "(globalThis.ran = (globalThis.ran || 0) + 1)") + `;
setTimeout(() => {` + queryScript(7, "(globalThis.ran = (globalThis.ran || 0) + 1)") + `}, 40);
setTimeout(() => console.log(String(globalThis.ran || 0)), 200);`
		out, err := exec.Command(node, "-e", js).CombinedOutput()
		if err != nil {
			t.Fatalf("node: %v\n%s", err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if got := run(false); got != "1" {
		t.Errorf("a resent script ran the expression %s times, want 1", got)
	}
	if got := run(true); got != "1" {
		t.Errorf("after a failed start, the resent script ran the expression %s times, want 1", got)
	}
}
