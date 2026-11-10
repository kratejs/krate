export async function GET() {
  const d = new Uint8Array(await crypto.subtle.digest("SHA-256", new TextEncoder().encode("abc")));
  const key = await crypto.subtle.importKey("raw", new TextEncoder().encode("k"), { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
  const sig = new Uint8Array(await crypto.subtle.sign("HMAC", key, new TextEncoder().encode("m")));
  let order = "";
  queueMicrotask(() => { order += "m"; });
  order += "s";
  await Promise.resolve();
  const ac = new AbortController(); ac.abort();
  return Response.json({
    b64: btoa("hello") + atob("aGk="),
    uuid: /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(crypto.randomUUID()),
    rnd: crypto.getRandomValues(new Uint8Array(8)).some((x) => x !== 0),
    digest: Array.from(d).map((b) => b.toString(16).padStart(2, "0")).join(""),
    hmac: Array.from(sig).map((b) => b.toString(16).padStart(2, "0")).join(""),
    clone: structuredClone({ d: new Date(5), m: new Map([[1, 2]]) }).m.get(1),
    order, aborted: ac.signal.aborted, perf: typeof performance.now(),
    dec: new TextDecoder().decode(new Uint8Array([104, 195, 169])),
    hdrs: [...new Headers({ B: "2", a: "1" })],
    sp: new URLSearchParams({ q: "a b&c" }).toString(),
    blob: await new Blob(["ab", "cd"]).text(),
  });
}
