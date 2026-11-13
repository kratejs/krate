package jsruntime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestQuickJSFetchOptions covers the embedded fetch polyfill: options reach
// the request (method, headers, body) and the response crosses the bridge.
func TestQuickJSFetchOptions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := new(strings.Builder)
		_, _ = fmt.Fprintf(b, "%s %s ", r.Method, r.Header.Get("X-Test"))
		buf := make([]byte, 64)
		n, _ := r.Body.Read(buf)
		b.Write(buf[:n])
		w.Header().Set("X-Echo", "1")
		_, _ = w.Write([]byte(b.String()))
	}))
	defer srv.Close()
	rt, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if _, err := rt.Execute(`var out; fetch(` + fmt.Sprintf("%q", srv.URL) + `, { method: "POST", headers: { "X-Test": "yes" }, body: "payload" })
	  .then(function (r) { return r.text().then(function (t) { out = r.status + " " + r.headers.get("X-Echo") + " " + t; }); }, function (e) { out = "ERR " + e; });`); err != nil {
		t.Fatal(err)
	}
	rt.DrainJobs()
	v, _ := rt.Execute("out")
	if v != "200 1 POST yes payload" {
		t.Fatalf("fetch result = %v", v)
	}
}
