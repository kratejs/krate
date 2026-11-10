export async function GET() {
  const fd = new FormData(); fd.append("k", "v");
  const list: [string, any][] = [["string", "s"], ["u8", new Uint8Array([104, 105])], ["ab", new Uint8Array([104, 105]).buffer], ["dv", new DataView(new Uint8Array([104, 105]).buffer)],
    ["blob", new Blob(["b"], { type: "Text/X-Blob" })], ["untypedBlob", new Blob(["b"])], ["usp", new URLSearchParams("a=1 2")], ["number", 42]];
  const out: Record<string, unknown> = {};
  for (const [k, v] of list) { const r = new Response(v); out[k] = [r.headers.get("content-type"), await r.text()]; }
  const fr = new Response(fd);
  out.formData = [/^multipart\/form-data; boundary=/.test(fr.headers.get("content-type") ?? ""), (await fr.text()).includes('name="k"')];
  out.explicit = new Response("s", { headers: { "content-type": "x/y" } }).headers.get("content-type");
  return Response.json(out);
}
