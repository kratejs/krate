function kind(e: any): string {
  if (typeof DOMException === "function" && e instanceof DOMException) return "DOMException:" + e.name;
  if (e instanceof TypeError) return "TypeError";
  if (e instanceof RangeError) return "RangeError";
  if (e instanceof SyntaxError) return "SyntaxError";
  if (e instanceof Error) return "Error:" + e.name;
  return "thrown:" + typeof e;
}
function errOf(fn: () => unknown): string {
  try { fn(); return "ok"; } catch (e) { return kind(e); }
}
async function errOfAsync(fn: () => unknown): Promise<string> {
  try { await fn(); return "ok"; } catch (e) { return kind(e); }
}
export async function GET() {
  const fd = await new Response("a=1&b=%20x", { headers: { "content-type": "application/x-www-form-urlencoded" } }).formData();
  const blob = await new Response("blob!", { headers: { "content-type": "text/x-thing" } }).blob();
  const ab = await new Response(new Uint8Array([1, 2, 3])).arrayBuffer();
  return Response.json({
    json: await new Response('{"a":[1]}').json(), badJson: await errOfAsync(() => new Response("{bad").json()),
    text: await new Response(new Uint8Array([0xe2, 0x82, 0xac])).text(),
    fd: [...fd], blob: [blob.size, blob.type, await blob.text()], ab: [ab instanceof ArrayBuffer, ab.byteLength],
    noFormType: await errOfAsync(() => new Response("a=1").formData()),
  });
}
