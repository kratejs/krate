package jsruntime

import (
	"context"
	"testing"
	"time"
)

// evalAsync runs an async JS expression (a function body returning a value)
// and returns its string form, driving timers like the API runner does.
func evalAsync(t *testing.T, rt *Runtime, body string) string {
	t.Helper()
	code := `globalThis.__t_out = undefined; globalThis.__t_err = undefined;
(async function(){ ` + body + ` })().then(function(v){ globalThis.__t_out = (typeof v === 'string') ? v : JSON.stringify(v); }, function(e){ globalThis.__t_err = String(e && e.stack || e); });`
	if _, err := rt.Execute(code); err != nil {
		t.Fatalf("exec: %v\n%s", err, body)
	}
	err := rt.waitFor(context.Background(), 5*time.Second, func() bool {
		v, _ := rt.Execute("globalThis.__t_out !== undefined || globalThis.__t_err !== undefined")
		b, _ := v.(bool)
		return b
	})
	if err != nil {
		t.Fatalf("wait: %v\n%s", err, body)
	}
	if v, _ := rt.Execute("globalThis.__t_err === undefined ? '' : globalThis.__t_err"); v != "" {
		return "ERR:" + v.(string)
	}
	v, _ := rt.Execute("String(globalThis.__t_out)")
	return v.(string)
}

func TestWebAPIMatrix(t *testing.T) {
	rt, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	cases := []struct{ name, body, want string }{
		{"headers case-insensitive", `const h = new Request("http://a/", {headers: {"Authorization": "x"}}).headers; return h.get("authorization") + h.has("AUTHORIZATION")`, "xtrue"},
		{"headers iterable sorted", `const h = new Headers({B: "2", a: "1"}); return JSON.stringify([...h])`, `[["a","1"],["b","2"]]`},
		{"headers multi", `const h = new Headers(); h.append("a","1"); h.append("a","2"); return h.get("a")`, "1, 2"},
		{"getSetCookie", `const h = new Headers(); h.append("Set-Cookie","a=1; Path=/"); h.append("set-cookie","b=2"); return JSON.stringify(h.getSetCookie()) + h.get("set-cookie")`, `["a=1; Path=/","b=2"]a=1; Path=/, b=2`},
		{"req json async", `const r = new Request("http://a/", {method:"POST", body: '{"a":1}'}); return JSON.stringify(await r.json())`, `{"a":1}`},
		{"req invalid json rejects", `const r = new Request("http://a/", {method:"POST", body: 'nope'}); try { await r.json(); return "no" } catch (e) { return e.name }`, "SyntaxError"},
		{"catch pattern", `const r = new Request("http://a/", {method:"POST", body: 'x'}); return String(await r.json().catch(() => null))`, "null"},
		{"body used", `const r = new Request("http://a/", {method:"POST", body: 'x'}); await r.text(); try { await r.text(); return "no" } catch (e) { return e.name }`, "TypeError"},
		{"formData urlencoded", `const r = new Request("http://a/", {method:"POST", body: "a=1&b=x+y&a=2", headers: {"content-type":"application/x-www-form-urlencoded"}}); const f = await r.formData(); return JSON.stringify([f.getAll("a"), f.get("b")])`, `[["1","2"],"x y"]`},
		{"formData multipart roundtrip", `const fd = new FormData(); fd.append("n","v"); fd.append("f", new Blob(["hi"],{type:"text/plain"}), "a.txt"); const req = new Request("http://a/", {method:"POST", body: fd}); const back = await req.formData(); const f = back.get("f"); return JSON.stringify([back.get("n"), f.name, f.type, await f.text()])`, `["v","a.txt","text/plain","hi"]`},
		{"arrayBuffer bytes", `const r = new Request("http://a/", {method:"POST", body: new Uint8Array([0,255,254,128])}); return JSON.stringify([...new Uint8Array(await r.arrayBuffer())])`, `[0,255,254,128]`},
		{"uint8array real", `const u = new Uint8Array([7,8]); return u[0] + "," + u.length + "," + (u instanceof Uint8Array)`, "7,2,true"},
		{"textencoder", `return JSON.stringify([...new TextEncoder().encode("he\u00e9\ud83d\ude00")])`, `[104,101,195,169,240,159,152,128]`},
		{"textdecoder", `return new TextDecoder().decode(new TextEncoder().encode("h\u00e9\ud83d\ude00"))`, "h\u00e9\U0001F600"},
		{"textdecoder fatal", `try { new TextDecoder("utf-8", {fatal:true}).decode(new Uint8Array([0xff])); return "no" } catch (e) { return e.name }`, "TypeError"},
		{"textdecoder stream", `const d = new TextDecoder(); return d.decode(new Uint8Array([0xe2,0x82]), {stream:true}) + "|" + d.decode(new Uint8Array([0xac]))`, "|\u20ac"},
		{"btoa atob", `return btoa("hello") + atob("aGVsbG8=")`, "aGVsbG8=hello"},
		{"atob invalid", `try { atob("***"); return "no" } catch (e) { return e.name }`, "InvalidCharacterError"},
		{"url parts", `const u = new URL("https://User:pw@Example.COM:8443/a/../b/./c?x=1&y=2#frag"); return [u.origin,u.host,u.hostname,u.port,u.pathname,u.search,u.hash,u.username,u.protocol].join(" ")`, "https://example.com:8443 example.com:8443 example.com 8443 /b/c ?x=1&y=2 #frag User https:"},
		{"url relative", `return new URL("/login", "http://blog.example.com/a/b").href`, "http://blog.example.com/login"},
		{"url invalid throws", `try { new URL("not a url"); return "no" } catch (e) { return e.name + ":" + e.message }`, "TypeError:Invalid URL"},
		{"url default port", `return new URL("http://a.com:80/x").host + "|" + new URL("https://a.com:443").href`, "a.com|https://a.com/"},
		{"url searchParams sync", `const u = new URL("http://a/?q=1"); u.searchParams.append("z","a b"); return u.search + "|" + u.href`, "?q=1&z=a+b|http://a/?q=1&z=a+b"},
		{"urlsearchparams eq", `const p = new URLSearchParams("e=x=y&p=a+b&s=%E2%82%AC&empty=&flag"); return JSON.stringify([p.get("e"), p.get("p"), p.get("s"), p.get("empty"), p.get("flag"), p.has("none")])`, `["x=y","a b","€","","",false]`},
		{"urlsearchparams toString", `return new URLSearchParams({a:"1 2", b:"&=", c:"\u00e9"}).toString()`, "a=1+2&b=%26%3D&c=%C3%A9"},
		{"canParse", `return String(URL.canParse("x")) + URL.canParse("http://x")`, "falsetrue"},
		{"settimeout fires", `const t0 = Date.now(); await new Promise(r => setTimeout(r, 30)); return String(Date.now() - t0 >= 25)`, "true"},
		{"timer order", `const out = []; await new Promise(r => { setTimeout(() => out.push("b"), 20); setTimeout(() => out.push("a"), 5); setTimeout(r, 40); }); return out.join("")`, "ab"},
		{"clearTimeout", `let hit = false; const t = setTimeout(() => { hit = true }, 10); clearTimeout(t); await new Promise(r => setTimeout(r, 30)); return String(hit)`, "false"},
		{"setInterval", `let n = 0; await new Promise(r => { const i = setInterval(() => { if (++n === 3) { clearInterval(i); r() } }, 5) }); return String(n)`, "3"},
		{"queueMicrotask", `let s = ""; queueMicrotask(() => { s += "m" }); s += "s"; await 0; return s`, "sm"},
		{"structuredClone", `const o = {a: [1,{b:new Date(5)}], m: new Map([[1,2]])}; o.self = o; const c = structuredClone(o); return [c !== o, c.self === c, c.a[1].b.getTime(), c.m.get(1)].join(",")`, "true,true,5,2"},
		{"structuredClone fn throws", `try { structuredClone({f(){}}); return "no" } catch (e) { return e.name }`, "DataCloneError"},
		{"performance", `return String(typeof performance.now() === "number" && performance.now() >= 0)`, "true"},
		{"abort", `const ac = new AbortController(); let fired = false; ac.signal.addEventListener("abort", () => { fired = true }); ac.abort(); return [ac.signal.aborted, fired, ac.signal.reason.name].join(",")`, "true,true,AbortError"},
		{"abort timeout", `const s = AbortSignal.timeout(10); await new Promise(r => setTimeout(r, 40)); return s.reason.name`, "TimeoutError"},
		{"response redirect", `const r = Response.redirect("http://a.com/x", 301); return r.status + r.headers.get("location")`, "301http://a.com/x"},
		{"response redirect relative throws", `try { Response.redirect("/x"); return "no" } catch (e) { return e.name }`, "TypeError"},
		{"response json", `const r = Response.json({a:1}, {status: 201}); return r.status + r.headers.get("content-type") + await r.text()`, `201application/json{"a":1}`},
		{"response arraybuffer", `const r = new Response(new Uint8Array([1,2,3])); return JSON.stringify([...new Uint8Array(await r.arrayBuffer())])`, "[1,2,3]"},
		{"response bad status", `try { new Response("x", {status: 99}); return "no" } catch (e) { return e.name }`, "RangeError"},
		{"stream body", `const rs = new ReadableStream({start(c){ c.enqueue(new TextEncoder().encode("a")); c.enqueue(new TextEncoder().encode("b")); c.close() }}); return await new Response(rs).text()`, "ab"},
		{"stream async iter", `const rs = new ReadableStream({pull(c){ c.enqueue("x"); c.close() }}); let s = ""; for await (const ch of rs) s += ch; return s`, "x"},
		{"blob", `const b = new Blob(["ab", new Uint8Array([99])], {type: "text/plain"}); return [b.size, b.type, await b.text(), await b.slice(1,2).text()].join(",")`, "3,text/plain,abc,b"},
		{"crypto uuid", `return String(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(crypto.randomUUID()))`, "true"},
		{"getRandomValues", `const a = new Uint8Array(16); crypto.getRandomValues(a); return String(a.some(x => x !== 0))`, "true"},
		{"digest", `const d = await crypto.subtle.digest("SHA-256", new TextEncoder().encode("abc")); return [...new Uint8Array(d)].map(b => b.toString(16).padStart(2,"0")).join("")`, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"},
		{"hmac", `const k = await crypto.subtle.importKey("raw", new TextEncoder().encode("key"), {name:"HMAC", hash:"SHA-256"}, false, ["sign","verify"]); const sig = await crypto.subtle.sign("HMAC", k, new TextEncoder().encode("msg")); const ok = await crypto.subtle.verify("HMAC", k, sig, new TextEncoder().encode("msg")); return [...new Uint8Array(sig)].map(b => b.toString(16).padStart(2,"0")).join("") + ok`, "2d93cbc1be167bcb1637a4a23cbff01a7878f0c50ee833954ea5221bb1b8c628true"},
		{"unsupported names API", `try { new TransformStream(); return "no" } catch (e) { return e.message }`, `TransformStream is not supported by the embedded API runtime; set ssr.apiRuntime to "node" in krate.config.ts to use it`},
		{"subtle unsupported", `try { await crypto.subtle.encrypt({name:"AES-GCM"}, null, new Uint8Array(1)); return "no" } catch (e) { return e.name }`, "NotSupportedError"},
		{"req cookies not on plain Request", `return String("cookies" in new Request("http://a/"))`, "false"},
		{"large bytes roundtrip", `const u = new Uint8Array(300000); for (let i = 0; i < u.length; i++) u[i] = i & 255; const back = new Uint8Array(await new Response(u).arrayBuffer()); let ok = back.length === u.length; for (let i = 0; i < u.length && ok; i++) ok = back[i] === u[i]; return String(ok)`, "true"},
	}
	for _, c := range cases {
		got := evalAsync(t, rt, c.body)
		if got != c.want {
			t.Errorf("%s:\n got  %q\n want %q", c.name, got, c.want)
		}
	}
}
