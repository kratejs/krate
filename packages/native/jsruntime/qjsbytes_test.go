package jsruntime

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	lib "modernc.org/libquickjs"
	"modernc.org/quickjs"
)

// withBase64 runs f with new VMs forced onto the base64 fallback.
func withBase64(t *testing.T, f func()) {
	t.Helper()
	forceBase64 = true
	defer func() { forceBase64 = false }()
	f()
}

func pending(vm *quickjs.VM) bool {
	tls, ctx := vmHandles(vm)
	return lib.XJS_HasException(tls, ctx) != 0
}

func setGlobalValue(t *testing.T, rt *Runtime, name string, v quickjs.Value) {
	t.Helper()
	atom, err := rt.vm.NewAtom(name)
	if err != nil {
		t.Fatal(err)
	}
	g := rt.vm.GlobalObject()
	defer g.Free()
	if err := rt.vm.SetPropertyValue(g, atom, v); err != nil {
		t.Fatal(err)
	}
}

func evalBytes(t *testing.T, rt *Runtime, code string) ([]byte, bool) {
	t.Helper()
	v, err := rt.vm.EvalValue(code, EvalGlobal)
	if err != nil {
		t.Fatalf("%s: %v", code, err)
	}
	defer v.Free()
	b, ok := valueBytes(rt.vm, v)
	if pending(rt.vm) {
		t.Fatalf("%s: valueBytes left an exception pending", code)
	}
	return b, ok
}

func TestBinaryTransferRoundTrip(t *testing.T) {
	if !binaryTransfer {
		t.Skip(binaryLayoutErr)
	}
	rt := mustRuntime(t, Options{})
	all := make([]byte, 256)
	for i := range all {
		all[i] = byte(i)
	}
	mb := bytes.Repeat([]byte{0, 7, 255, 128}, 1<<18)
	for _, c := range []struct {
		name string
		data []byte
	}{{"empty", []byte{}}, {"one byte", []byte{0}}, {"all values", all}, {"1 MiB", mb}} {
		t.Run(c.name, func(t *testing.T) {
			v, ok := newArrayBuffer(rt.vm, c.data)
			if !ok {
				t.Fatal("newArrayBuffer failed")
			}
			setGlobalValue(t, rt, "__t_in", v)
			v.Free()
			// JS samples every 4099th byte (all of them for the small cases).
			var sum int
			for i := 0; i < len(c.data); i += 4099 {
				sum += int(c.data[i])
			}
			if len(c.data) <= 4099 {
				sum = 0
				for _, b := range c.data {
					sum += int(b)
				}
			}
			got := execString(t, rt, `(function () { const u = new Uint8Array(globalThis.__t_in); const step = u.length <= 4099 ? 1 : 4099; let s = 0; for (let i = 0; i < u.length; i += step) s += u[i]; return (globalThis.__t_in instanceof ArrayBuffer) + ":" + u.length + ":" + s; })()`)
			if want := fmt.Sprintf("true:%d:%d", len(c.data), sum); got != want {
				t.Fatalf("JS saw %s, want %s", got, want)
			}
			for _, expr := range []string{"globalThis.__t_in", "new Uint8Array(globalThis.__t_in)"} {
				back, ok := evalBytes(t, rt, expr)
				if !ok || !bytes.Equal(back, c.data) {
					t.Fatalf("%s: read %d bytes (ok %v), want %d", expr, len(back), ok, len(c.data))
				}
			}
		})
	}
}

// binaryHostCases run in both transfer modes and must give the same answers.
var binaryHostCases = []struct{ name, body, want string }{
	{"digest", `const d = await crypto.subtle.digest("SHA-256", new TextEncoder().encode("abc").subarray(1)); return [...new Uint8Array(d)].map(b => b.toString(16).padStart(2, "0")).join("")`, hexSHA256("bc")},
	{"digest all bytes", `const u = new Uint8Array(256).map((_, i) => i); const d = await crypto.subtle.digest("SHA-256", u.buffer); return [...new Uint8Array(d)].map(b => b.toString(16).padStart(2, "0")).join("")`, hexSHA256(string(allBytes()))},
	{"hmac", `const k = await crypto.subtle.importKey("raw", new Uint8Array([0, 1, 255]), { name: "HMAC", hash: "SHA-256" }, false, ["sign"]); const s = await crypto.subtle.sign("HMAC", k, new Uint8Array([9, 0, 9])); return [...new Uint8Array(s)].map(b => b.toString(16).padStart(2, "0")).join("")`, hexHMAC([]byte{0, 1, 255}, []byte{9, 0, 9})},
	{"random", `const a = crypto.getRandomValues(new Uint8Array(64)); const b = crypto.getRandomValues(new Uint8Array(64)); return a.length + ":" + (a.join() !== b.join())`, "64:true"},
	{"uuid", `return /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(crypto.randomUUID())`, "true"},
	{"multipart", `const fd = new FormData(); fd.append("n", "v\u00e9"); fd.append("f", new Blob([new Uint8Array([0, 255, 0, 13, 10])]), "a.bin"); fd.append("e", new Blob([]), "e.txt"); const back = await new Response(fd).formData(); const f = back.get("f"); return JSON.stringify([back.get("n"), f.name, f.type, [...new Uint8Array(await f.arrayBuffer())], back.get("e").size])`, "[\"v\u00e9\",\"a.bin\",\"application/octet-stream\",[0,255,0,13,10],0]"},
	{"multipart error", `try { await new Response("x", { headers: { "content-type": "multipart/form-data; boundary=zz" } }).formData(); return "no"; } catch (e) { return e.name + ": " + e.message.split(":")[0]; }`, "TypeError: Could not parse content as FormData"},
	{"host error", `const K = globalThis[Symbol.for("krate.web")]; K.need("crypto"); try { K.host(__go_digest, "md5", K.toHost(new Uint8Array(1))); return "no"; } catch (e) { return e.message; }`, `unsupported hash "md5"`},
	{"host error is consumed", `const K = globalThis[Symbol.for("krate.web")]; K.need("crypto"); try { K.host(__go_digest, "md5", K.toHost(new Uint8Array(1))); } catch (e) {} return JSON.stringify(__go_host_error())`, `""`},
}

func allBytes() []byte {
	b := make([]byte, 256)
	for i := range b {
		b[i] = byte(i)
	}
	return b
}

func hexSHA256(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func hexHMAC(key, data []byte) string {
	m := hmac.New(sha256.New, key)
	m.Write(data)
	return hex.EncodeToString(m.Sum(nil))
}

func TestBinaryHostFunctions(t *testing.T) {
	for _, mode := range []string{"arraybuffer", "base64"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "arraybuffer" && !binaryTransfer {
				t.Skip(binaryLayoutErr)
			}
			var rt *Runtime
			mk := func() { rt = mustRuntime(t, Options{}) }
			if mode == "base64" {
				withBase64(t, mk)
			} else {
				mk()
			}
			if rt.bin != (mode == "arraybuffer") {
				t.Fatalf("bin = %v", rt.bin)
			}
			for _, c := range binaryHostCases {
				if got := evalAsync(t, rt, c.body); got != c.want {
					t.Errorf("%s: got %q, want %q", c.name, got, c.want)
				}
			}
		})
	}
}

func TestBinaryFetch(t *testing.T) {
	payload := func(n int) []byte {
		p := make([]byte, n)
		for i := range p {
			p[i] = byte(i * 31)
		}
		return p
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("x-got", fmt.Sprint(len(body), ":", hexSHA256(string(body))))
		var n int
		_, _ = fmt.Sscan(r.URL.Query().Get("n"), &n)
		_, _ = w.Write(payload(n))
	}))
	defer srv.Close()
	for _, mode := range []string{"arraybuffer", "base64"} {
		t.Run(mode, func(t *testing.T) {
			// base64 is slow in the VM: a smaller body covers the fallback.
			n := 300000
			if mode == "arraybuffer" && !binaryTransfer {
				t.Skip(binaryLayoutErr)
			}
			var rt *Runtime
			mk := func() { rt = mustRuntime(t, Options{}) }
			if mode == "base64" {
				n = 20000
				withBase64(t, mk)
			} else {
				mk()
			}
			url := fmt.Sprintf("%s/?n=%d", srv.URL, n)
			body := `const res = await fetch("` + url + `", { method: "POST", body: new Uint8Array(256).map((_, i) => i).subarray(1) });
const b = new Uint8Array(await res.arrayBuffer());
let s = 0; for (let i = 0; i < b.length; i += 97) s = (s + b[i] * (i % 7 + 1)) % 1000003;
return res.headers.get("x-got") + "|" + b.length + "|" + s`
			var sum int
			p := payload(n)
			for i := 0; i < len(p); i += 97 {
				sum = (sum + int(p[i])*(i%7+1)) % 1000003
			}
			want := fmt.Sprint(255, ":", hexSHA256(string(allBytes()[1:])), "|", n, "|", sum)
			if got := evalAsync(t, rt, body); got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
			rt.dataCache = NewDataCache(10)
			cached := `const a = await (await fetch("` + url + `", { cache: "force-cache" })).arrayBuffer();
const b = await (await fetch("` + url + `", { cache: "force-cache" })).arrayBuffer();
const x = new Uint8Array(a), y = new Uint8Array(b);
let same = x.length === y.length; for (let i = 0; same && i < x.length; i += 97) same = x[i] === y[i];
return a.byteLength + ":" + b.byteLength + ":" + same`
			if got, want := evalAsync(t, rt, cached), fmt.Sprint(n, ":", n, ":true"); got != want {
				t.Fatalf("cached fetch: got %q, want %q", got, want)
			}
		})
	}
}

// binaryRouteDir writes routes that read and write binary bodies through
// every runner path: buffered, streamed, multipart and legacy text.
func binaryRouteDir(t *testing.T) string {
	t.Helper()
	apiDir := filepath.Join(t.TempDir(), "api")
	if err := os.MkdirAll(apiDir, 0755); err != nil {
		t.Fatal(err)
	}
	routes := map[string]string{
		"echo.js":   `async function POST(request) { return new Response(await request.arrayBuffer(), { headers: { "content-type": "application/octet-stream" } }); }`,
		"bytes.js":  `async function POST(request) { const b = await request.bytes(); return new Response(b.subarray(1)); }`,
		"text.js":   `async function POST(request) { return new Response(await request.text()); }`,
		"stream.js": `async function POST(request) { const b = new Uint8Array(await request.arrayBuffer()); let i = 0; return new Response(new ReadableStream({ pull(c) { if (i >= b.length) { c.close(); return; } c.enqueue(b.subarray(i, i + 65536)); i += 65536; } })); }`,
		"form.js":   `async function POST(request) { const fd = await request.formData(); const f = fd.get("f"); return new Response(await f.arrayBuffer()); }`,
		"nul.js":    `function GET() { return new Response("a\0b"); }`,
	}
	for name, code := range routes {
		if err := os.WriteFile(filepath.Join(apiDir, name), []byte(code), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return apiDir
}

func TestBinaryAPIBodies(t *testing.T) {
	text := strings.Repeat("h\u00e9\U0001F600", 1000)
	for _, mode := range []string{"arraybuffer", "base64"} {
		t.Run(mode, func(t *testing.T) {
			// base64 is slow in the VM: a smaller body covers the fallback.
			n := 1<<20 + 5
			if mode == "arraybuffer" && !binaryTransfer {
				t.Skip(binaryLayoutErr)
			}
			if mode == "base64" {
				n = 1<<14 + 5
				forceBase64 = true
				defer func() { forceBase64 = false }()
			}
			data := make([]byte, n)
			for i := range data {
				data[i] = byte(i ^ i>>8)
			}
			var form bytes.Buffer
			form.WriteString("--BB\r\nContent-Disposition: form-data; name=\"f\"; filename=\"a.bin\"\r\n\r\n")
			form.Write(data)
			form.WriteString("\r\n--BB--\r\n")
			rt := NewAPIRouteRuntime(binaryRouteDir(t))
			rt.SetPool(1, false)
			for _, c := range []struct {
				route, ct  string
				body, want []byte
			}{
				{"echo", "application/octet-stream", data, data},
				{"echo", "application/octet-stream", []byte{0}, []byte{0}},
				{"bytes", "application/octet-stream", data, data[1:]},
				{"text", "text/plain", []byte(text), []byte(text)},
				{"stream", "application/octet-stream", data, data},
				{"form", "multipart/form-data; boundary=BB", form.Bytes(), data},
				{"nul", "", nil, []byte("a\x00b")},
			} {
				method := "POST"
				if c.body == nil {
					method = "GET"
				}
				r := rt.Execute(APIRequest{Method: method, Path: "/api/" + c.route, Headers: map[string][]string{"Content-Type": {c.ct}}, Body: c.body})
				if r.Status != 200 || !bytes.Equal(r.Body, c.want) {
					t.Errorf("%s (%d bytes): status %d, %d bytes back, want %d (%s)", c.route, len(c.body), r.Status, len(r.Body), len(c.want), r.Error)
				}
			}
		})
	}
}
