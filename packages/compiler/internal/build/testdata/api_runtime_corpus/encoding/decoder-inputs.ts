export async function GET() {
  const t = new TextDecoder();
  const buf = new TextEncoder().encode("hello world").buffer;
  let bad = "ok";
  try { t.decode("str" as any); } catch (e) { bad = e instanceof TypeError ? "TypeError" : e.name; }
  return Response.json({
    arrayBuffer: t.decode(buf),
    dataView: t.decode(new DataView(buf, 6, 5)),
    subarray: t.decode(new Uint8Array(buf).subarray(0, 5)),
    u16: t.decode(new Uint16Array([0x6968])),
    none: t.decode(),
    undef: t.decode(undefined),
    bad,
  });
}
