package jsruntime

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBodyHeadroom serves bodies well over a small base memory limit through
// each way a handler reads them, one after another on one pooled worker: the
// headroom granted at read time must cover each, and the collection after
// each request must leave room for the next.
func TestBodyHeadroom(t *testing.T) {
	t.Setenv("KRATE_JS_MEMORY_MB", "4")
	n := 8 << 20
	bin := bytes.Repeat([]byte{0, 1, 2, 200}, n/4)
	ascii := []byte(strings.Repeat("abcd", n/4))
	wide := []byte(strings.Repeat("é中", n/5))
	var form bytes.Buffer
	form.WriteString("--BB\r\nContent-Disposition: form-data; name=\"f\"; filename=\"a.bin\"\r\n\r\n")
	form.Write(bin)
	form.WriteString("\r\n--BB--\r\n")
	routes := map[string]string{
		"len":    `async function POST(r) { const b = await r.arrayBuffer(); return Response.json({ n: b.byteLength }); }`,
		"echo":   `async function POST(r) { return new Response(await r.arrayBuffer()); }`,
		"text":   `async function POST(r) { return new Response(await r.text()); }`,
		"json":   `async function POST(r) { const j = await r.json(); return Response.json({ n: j.a.length }); }`,
		"form":   `async function POST(r) { const f = (await r.formData()).get("f"); return new Response(f); }`,
		"ignore": `function POST() { return new Response("ok"); }`,
	}
	apiDir := filepath.Join(t.TempDir(), "api")
	if err := os.MkdirAll(apiDir, 0755); err != nil {
		t.Fatal(err)
	}
	for k, v := range routes {
		if err := os.WriteFile(filepath.Join(apiDir, k+".js"), []byte(v), 0644); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		route, ct  string
		body, want []byte
	}{
		{"len", "", bin, []byte(`{"n":8388608}`)},
		{"echo", "", bin, bin},
		{"text", "", ascii, ascii},
		{"text", "", wide, wide},
		{"json", "", []byte(`{"a":"` + strings.Repeat("x", n-8) + `"}`), []byte(`{"n":8388600}`)},
		{"form", "multipart/form-data; boundary=BB", form.Bytes(), bin},
		{"ignore", "", bin, []byte("ok")},
	}
	if !binaryTransfer {
		t.Skip(binaryLayoutErr)
	}
	rt := NewAPIRouteRuntime(apiDir)
	rt.SetPool(1, false)
	for _, c := range cases {
		r := rt.Execute(APIRequest{Method: "POST", Path: "/api/" + c.route, Headers: map[string][]string{"Content-Type": {c.ct}}, Body: c.body})
		if r.Status != 200 || !bytes.Equal(r.Body, c.want) {
			t.Errorf("%s (%d bytes): status %d, %d bytes back (%s) %q", c.route, len(c.body), r.Status, len(r.Body), r.Error, clipBytes(r.Body))
		}
	}
}

// TestBodyHeadroomGrantedOnRead checks the limit grows only once the body is
// read, by the factor of the form it is read in, and never shrinks mid-request.
func TestBodyHeadroomGrantedOnRead(t *testing.T) {
	if !binaryTransfer {
		t.Skip(binaryLayoutErr)
	}
	rt := mustRuntime(t, Options{})
	if err := rt.registerBodyHost(); err != nil {
		t.Fatal(err)
	}
	rt.curBody = make([]byte, 1000)
	if rt.headroom != 0 {
		t.Fatalf("headroom %d before any read", rt.headroom)
	}
	execString(t, rt, `String(new Uint8Array(__go_body_bytes()).length)`)
	if rt.headroom != 1000*headroomBytes {
		t.Fatalf("headroom %d after a bytes read, want %d", rt.headroom, 1000*headroomBytes)
	}
	execString(t, rt, `__go_body_text()`)
	if rt.headroom != 1000*headroomText {
		t.Fatalf("headroom %d after a text read, want %d", rt.headroom, 1000*headroomText)
	}
	execString(t, rt, `String(new Uint8Array(__go_body_bytes()).length)`)
	if rt.headroom != 1000*headroomText {
		t.Fatalf("headroom shrank to %d", rt.headroom)
	}
}

func clipBytes(b []byte) []byte {
	if len(b) > 200 {
		return b[:200]
	}
	return b
}
