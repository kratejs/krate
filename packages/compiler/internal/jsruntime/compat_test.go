package jsruntime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

var compatAPIPath = regexp.MustCompile(`^[A-Za-z_$][\w$]*(\.[A-Za-z_$][\w$]*)*$`)

// compatTestSources indexes every *_test.go file in the compiler module: the
// Test functions it declares and its full text (for subtest names).
var compatTestSources = sync.OnceValues(func() (map[string]bool, string) {
	funcs := map[string]bool{}
	var all strings.Builder
	decl := regexp.MustCompile(`(?m)^func (Test\w+)\(t \*testing\.T\)`)
	_ = filepath.WalkDir(filepath.Join("..", ".."), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == "testdata") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		for _, m := range decl.FindAllStringSubmatch(string(data), -1) {
			funcs[m[1]] = true
		}
		all.Write(data)
		return nil
	})
	return funcs, all.String()
})

func TestCompatLedger(t *testing.T) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(compatJSON, &raw); err != nil {
		t.Fatalf("compat.json: %v", err)
	}
	for k := range raw {
		switch k {
		case "$comment", "denominator", "features", "rows":
		default:
			t.Errorf("compat.json: unknown top-level key %q", k)
		}
	}
	l, err := parseCompatLedger(compatJSON)
	if err != nil {
		t.Fatal(err)
	}
	if l.Denominator == "" || len(l.Rows) == 0 || len(l.Features) == 0 {
		t.Fatal("compat.json: denominator, features and rows are required")
	}
	globalFeature := map[string]string{}
	for name, f := range l.Features {
		if f.Title == "" || f.Section == "" || f.Description == "" {
			t.Errorf("feature %s: title, section and description are required", name)
		}
		for _, g := range f.Globals {
			if other, dup := globalFeature[g]; dup {
				t.Errorf("global %s is listed by features %s and %s", g, other, name)
			}
			globalFeature[g] = name
		}
		for _, req := range f.Requires {
			if _, ok := l.Features[req]; !ok || req == name {
				t.Errorf("feature %s requires unknown feature %q", name, req)
			}
		}
	}
	funcs, text := compatTestSources()
	used := map[string]bool{}
	for _, r := range l.Rows {
		if !compatAPIPath.MatchString(r.API) && r.API != "node:*" {
			t.Errorf("%s: api must be a global path or node:*", r.API)
		}
		if _, ok := l.Features[r.Feature]; !ok {
			t.Errorf("%s: unknown feature %q", r.API, r.Feature)
		}
		used[r.Feature] = true
		if !strings.Contains(r.API, ".") && len(l.Features[r.Feature].Globals) > 0 {
			listed := globalFeature[r.API] == r.Feature
			if r.Status != CompatNotSupported && !listed {
				t.Errorf("%s: %s row, but feature %s does not list it in globals", r.API, r.Status, r.Feature)
			}
		}
		switch r.Status {
		case CompatSupported, CompatPartial:
			if len(r.Tests) == 0 {
				t.Errorf("%s: %s rows must list at least one test", r.API, r.Status)
			}
		case CompatNotSupported, CompatByDesign:
			if r.Hint == "" {
				t.Errorf("%s: %s rows need a hint", r.API, r.Status)
			}
		default:
			t.Errorf("%s: unknown status %q", r.API, r.Status)
		}
		if r.Status == CompatPartial && r.Notes == "" {
			t.Errorf("%s: partial rows must say what is missing in notes", r.API)
		}
		for _, name := range r.Tests {
			if c, ok := strings.CutPrefix(name, "corpus:"); ok {
				spec, err := readCorpusSpec(c)
				if err != nil {
					t.Errorf("%s: %v", r.API, err)
					continue
				}
				if spec.Divergence != nil && r.Status == CompatSupported {
					t.Errorf("%s: cites %s, which records a divergence from Node (%s), so the row must be partial", r.API, name, spec.Divergence.Reason)
				}
				continue
			}
			fn, sub, _ := strings.Cut(name, "/")
			if !funcs[fn] {
				t.Errorf("%s: test %s does not exist", r.API, fn)
			}
			if sub != "" && !strings.Contains(text, `"`+sub+`"`) {
				t.Errorf("%s: subtest %q of %s not found", r.API, sub, fn)
			}
		}
		for _, s := range []string{r.Notes, r.Hint} {
			for _, c := range s {
				if c > 127 {
					t.Errorf("%s: notes and hints must be plain ASCII", r.API)
					break
				}
			}
		}
	}
	for name := range l.Features {
		if !used[name] {
			t.Errorf("feature %s has no rows", name)
		}
	}
	for g, f := range globalFeature {
		if r, ok := l.Lookup(g); !ok || r.Feature != f {
			t.Errorf("feature %s lists global %s, which has no row in that feature", f, g)
		}
	}
}

// corpusDir holds the QuickJS vs Node differential corpus that ledger rows
// cite as corpus:<case>.
var corpusDir = filepath.Join("..", "build", "testdata", "api_runtime_corpus")

type corpusSpec struct {
	Divergence *struct {
		Reason string `json:"reason"`
	} `json:"divergence"`
}

func readCorpusSpec(name string) (*corpusSpec, error) {
	if name == "" || strings.Contains(name, "..") || strings.ContainsAny(name, "*?") {
		return nil, fmt.Errorf("corpus:%s must name one case", name)
	}
	base := filepath.Join(corpusDir, filepath.FromSlash(name))
	if _, err := os.Stat(base + ".ts"); err != nil {
		return nil, fmt.Errorf("corpus case %s has no route module: %w", name, err)
	}
	data, err := os.ReadFile(base + ".json")
	if err != nil {
		return nil, fmt.Errorf("corpus case %s has no request spec: %w", name, err)
	}
	var spec corpusSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("corpus case %s: %w", name, err)
	}
	return &spec, nil
}

// TestCompatLedgerMatchesPrelude checks the ledger against a real VM:
// supported and partial APIs are defined and are not stubs, and not-supported
// ones are undefined or stubs that throw NotSupportedError with KR7160.
func TestCompatLedgerMatchesPrelude(t *testing.T) {
	for _, mode := range []string{"lazy", "preloaded"} {
		t.Run(mode, func(t *testing.T) {
			rt, err := NewWithOptions(Options{PreloadAll: mode == "preloaded"})
			if err != nil {
				t.Fatal(err)
			}
			defer rt.Close()
			checkLedgerAgainstVM(t, rt)
		})
	}
}

func checkLedgerAgainstVM(t *testing.T, rt *Runtime) {
	for _, r := range Compat().Rows {
		if !compatAPIPath.MatchString(r.API) {
			continue
		}
		path, _ := json.Marshal(r.API)
		probe := `(function () {
  var segs = ` + string(path) + `.split('.'), v = globalThis;
  for (var i = 0; i < segs.length; i++) { if (v === undefined || v === null) return 'undefined'; v = v[segs[i]]; }
  if (v === undefined) return 'undefined';
  if (v !== null && (typeof v === 'function' || typeof v === 'object') && v[Symbol.for('krate.unsupported')]) return 'stub';
  return 'defined';
})()`
		got, err := rt.Execute(probe)
		if err != nil {
			t.Fatalf("%s: %v", r.API, err)
		}
		switch r.Status {
		case CompatSupported, CompatPartial:
			if got != "defined" {
				t.Errorf("%s is %s in the ledger but %v in the VM", r.API, r.Status, got)
			}
		case CompatNotSupported:
			switch got {
			case "undefined":
			case "stub":
				res := evalAsync(t, rt, `const segs = `+string(path)+`.split("."); let holder = globalThis;
for (let i = 0; i < segs.length - 1; i++) holder = holder[segs[i]];
try { await holder[segs[segs.length - 1]].call(holder); return "no error"; } catch (e) { return e.name + ": " + e.message; }`)
				if !strings.HasPrefix(res, "NotSupportedError: [KR7160] ") {
					t.Errorf("%s stub threw %q, want a NotSupportedError carrying KR7160", r.API, res)
				}
			default:
				t.Errorf("%s is not-supported in the ledger but defined in the VM", r.API)
			}
		}
	}
}

// compatBehavior holds one small behavior check per supported or partial row;
// each returns "ok" when the API behaves as the ledger says.
var compatBehavior = map[string]string{
	"DOMException":    `const e = new DOMException("m", "AbortError"); return e.name === "AbortError" && e.code === 20 && e instanceof Error ? "ok" : "bad"`,
	"Event":           `const e = new Event("x", { cancelable: true }); e.preventDefault(); return e.type === "x" && e.defaultPrevented ? "ok" : "bad"`,
	"EventTarget":     `const t = new EventTarget(); let n = 0; t.addEventListener("a", () => n++, { once: true }); t.dispatchEvent(new Event("a")); t.dispatchEvent(new Event("a")); return n === 1 ? "ok" : "bad"`,
	"AbortController": `const c = new AbortController(); c.abort("r"); return c.signal.aborted && c.signal.reason === "r" ? "ok" : "bad"`,
	"AbortSignal":     `const s = AbortSignal.any([AbortSignal.abort("x")]); try { s.throwIfAborted(); return "bad" } catch (e) { return e === "x" ? "ok" : "bad" }`,
	"setTimeout":      `return await new Promise((r) => setTimeout(r, 1, "ok"))`,
	"clearTimeout":    `let hit = false; clearTimeout(setTimeout(() => { hit = true }, 1)); await new Promise((r) => setTimeout(r, 5)); return hit ? "bad" : "ok"`,
	"setInterval":     `let n = 0; await new Promise((r) => { const h = setInterval(() => { if (++n === 3) { clearInterval(h); r() } }, 1) }); return n === 3 ? "ok" : "bad"`,
	"clearInterval":   `let n = 0; clearInterval(setInterval(() => n++, 1)); await new Promise((r) => setTimeout(r, 5)); return n === 0 ? "ok" : "bad"`,
	"setImmediate":    `return await new Promise((r) => setImmediate(r, "ok"))`,
	"clearImmediate":  `let hit = false; clearImmediate(setImmediate(() => { hit = true })); await new Promise((r) => setTimeout(r, 5)); return hit ? "bad" : "ok"`,
	"queueMicrotask":  `const o = []; queueMicrotask(() => o.push(2)); o.push(1); await null; return o.join() === "1,2" ? "ok" : o.join()`,
	"performance":     `const a = performance.now(); return typeof a === "number" && performance.timeOrigin > 0 && performance.mark === undefined ? "ok" : "bad"`,
	"atob":            `return atob("aGk=") === "hi" ? "ok" : "bad"`,
	"btoa":            `return btoa("hi") === "aGk=" ? "ok" : "bad"`,
	"TextEncoder":     `const u = new Uint8Array(4); const r = new TextEncoder().encodeInto("hé", u); return r.read === 2 && r.written === 3 ? "ok" : "bad"`,
	"TextDecoder": `const a = new TextDecoder("latin1").decode(new Uint8Array([0xe9])); const b = new TextDecoder("utf-16le").decode(new Uint8Array([104, 0]));
try { new TextDecoder("shift_jis"); return "bad" } catch (e) { return a === "é" && b === "h" && e.name === "RangeError" ? "ok" : "bad" }`,
	"structuredClone": `const a = { d: new Date(5), m: new Map([[1, 2]]) }; a.self = a; const b = structuredClone(a); let t = "";
try { structuredClone(1, { transfer: [new ArrayBuffer(1)] }) } catch (e) { t = e.name }
return b.self === b && b.d.getTime() === 5 && b.m.get(1) === 2 && t === "NotSupportedError" ? "ok" : "bad"`,
	"URL":             `return URL.canParse("http://a") && URL.parse("nope") === null && new URL("../b", "http://a/c/d").href === "http://a/b" ? "ok" : "bad"`,
	"URLSearchParams": `const p = new URLSearchParams("a=1&b=2&a=3"); p.append("c", "x y"); return p.getAll("a").join() === "1,3" && p.toString() === "a=1&b=2&a=3&c=x+y" ? "ok" : p.toString()`,
	"Headers":         `const h = new Headers([["X-A", "1"]]); h.append("x-a", "2"); return h.get("X-A") === "1, 2" ? "ok" : "bad"`,
	"Request": `const r = new Request("http://a/x", { method: "POST", body: "hi" }); const c = r.clone();
const s = new Request("http://a/", { method: "POST", body: new ReadableStream({ start(c) { c.close() } }), duplex: "half" });
let t = ""; try { s.clone() } catch (e) { t = e.name }
return r.method === "POST" && (await c.text()) === "hi" && (await r.text()) === "hi" && t === "NotSupportedError" ? "ok" : "bad " + t`,
	"Response": `const r = Response.json({ a: 1 }, { status: 201 }); const red = Response.redirect("http://a/", 307);
return r.status === 201 && r.headers.get("content-type").startsWith("application/json") && (await r.text()) === '{"a":1}' && red.headers.get("location") === "http://a/" && Response.error().type === "error" ? "ok" : "bad"`,
	"fetch": `let a = "", b = ""; try { await fetch("not a url") } catch (e) { a = e.name }
try { await fetch("http://a/", { signal: AbortSignal.abort() }) } catch (e) { b = e.name }
return a === "TypeError" && b === "AbortError" ? "ok" : a + b`,
	"Blob":     `const b = new Blob(["ab", new Uint8Array([99])], { type: "T" }); return b.size === 3 && b.type === "t" && (await b.slice(1).text()) === "bc" ? "ok" : "bad"`,
	"File":     `const f = new File(["x"], "a.txt", { type: "text/plain", lastModified: 7 }); return f.name === "a.txt" && f.lastModified === 7 && f instanceof Blob ? "ok" : "bad"`,
	"FormData": `const f = new FormData(); f.append("a", "1"); f.set("a", "2"); f.append("b", new Blob(["z"])); return [...f.keys()].join() === "a,b" && f.get("a") === "2" && f.get("b").name === "blob" ? "ok" : "bad"`,
	"ReadableStream": `const s = ReadableStream.from(["a", "b"]); let o = ""; for await (const c of s) o += c;
let t = ""; try { new ReadableStream().tee() } catch (e) { t = e.name }
return o === "ab" && t === "NotSupportedError" ? "ok" : "bad"`,
	"ReadableStreamDefaultReader": `const r = new ReadableStreamDefaultReader(new ReadableStream({ start(c) { c.enqueue(1); c.close() } })); const a = await r.read(); const b = await r.read(); return a.value === 1 && b.done ? "ok" : "bad"`,
	"crypto":                      `return typeof crypto.getRandomValues === "function" && typeof crypto.randomUUID === "function" && typeof crypto.subtle === "object" ? "ok" : "bad"`,
	"crypto.getRandomValues":      `const a = new Uint8Array(32); crypto.getRandomValues(a); return a.some((x) => x !== 0) ? "ok" : "bad"`,
	"crypto.randomUUID":           `return /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(crypto.randomUUID()) ? "ok" : "bad"`,
	"crypto.subtle":               `let t = ""; try { await crypto.subtle.encrypt({ name: "AES-GCM" }, null, new Uint8Array(1)) } catch (e) { t = e.name } return typeof crypto.subtle.digest === "function" && t === "NotSupportedError" ? "ok" : "bad"`,
	"crypto.subtle.digest":        `const d = await crypto.subtle.digest("SHA-256", new TextEncoder().encode("abc")); return [...new Uint8Array(d)].slice(0, 4).map((x) => x.toString(16).padStart(2, "0")).join("") === "ba7816bf" ? "ok" : "bad"`,
	"crypto.subtle.importKey": `const k = await crypto.subtle.importKey("raw", new Uint8Array([1]), { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
let t = ""; try { await crypto.subtle.importKey("jwk", {}, { name: "HMAC" }, false, []) } catch (e) { t = e.name }
return k.type === "secret" && k.algorithm.name === "HMAC" && t === "NotSupportedError" ? "ok" : "bad"`,
	"crypto.subtle.sign": `const k = await crypto.subtle.importKey("raw", new TextEncoder().encode("key"), { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
const s = await crypto.subtle.sign("HMAC", k, new TextEncoder().encode("The quick brown fox jumps over the lazy dog"));
return [...new Uint8Array(s)].slice(0, 4).map((x) => x.toString(16).padStart(2, "0")).join("") === "f7bc83f4" ? "ok" : "bad"`,
	"crypto.subtle.verify": `const k = await crypto.subtle.importKey("raw", new Uint8Array([1, 2]), { name: "HMAC", hash: "SHA-256" }, false, ["sign", "verify"]);
const data = new Uint8Array([3]); const s = new Uint8Array(await crypto.subtle.sign("HMAC", k, data));
const good = await crypto.subtle.verify("HMAC", k, s, data); s[0] ^= 1; const bad = await crypto.subtle.verify("HMAC", k, s, data);
return good && !bad ? "ok" : "bad"`,
	"CryptoKey":   `const k = await crypto.subtle.importKey("raw", new Uint8Array([1]), { name: "HMAC", hash: "SHA-1" }, false, ["sign"]); return k instanceof CryptoKey ? "ok" : "bad"`,
	"console":     `return ["log", "info", "debug", "warn", "error", "trace"].every((m) => typeof console[m] === "function") && console.table === undefined ? "ok" : "bad"`,
	"globalThis":  `return globalThis.Object === Object ? "ok" : "bad"`,
	"process":     `return Object.keys(process).join() === "env" ? "ok" : Object.keys(process).join()`,
	"process.env": `return typeof process.env === "object" && process.env !== null ? "ok" : "bad"`,
	"require":     `const p = require("path"); let t = ""; try { require("os") } catch (e) { t = e.message } return p.basename("/a/b.txt", ".txt") === "b" && typeof require("fs").readFileSync === "function" && t.includes("os") ? "ok" : "bad"`,
}

// TestCompatLedgerBehavior runs each check in a VM that preloads every
// feature, and again in a fresh VM that loads only what the check uses (so a
// feature that misses one it depends on fails here).
func TestCompatLedgerBehavior(t *testing.T) {
	rt, err := NewWithOptions(Options{PreloadAll: true})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	rows := map[string]bool{}
	for _, r := range Compat().Rows {
		if r.Status != CompatSupported && r.Status != CompatPartial {
			continue
		}
		rows[r.API] = true
		body, ok := compatBehavior[r.API]
		if !ok {
			t.Errorf("%s: no behavior check in compatBehavior", r.API)
			continue
		}
		t.Run(r.API, func(t *testing.T) {
			if got := evalAsync(t, rt, body); got != "ok" {
				t.Errorf("got %q", got)
			}
			fresh, err := New()
			if err != nil {
				t.Fatal(err)
			}
			defer fresh.Close()
			if got := evalAsync(t, fresh, body); got != "ok" {
				t.Errorf("in a lazily set up VM: got %q", got)
			}
		})
	}
	for api := range compatBehavior {
		if !rows[api] {
			t.Errorf("compatBehavior has %s, which is not a supported or partial row", api)
		}
	}
}
