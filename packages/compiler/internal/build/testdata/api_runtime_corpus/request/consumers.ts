export async function POST(request) {
  const q = new URL(request.url).searchParams.get("as");
  if (q === "json") return Response.json({ v: await request.json() });
  if (q === "ab") { const b = await request.arrayBuffer(); return Response.json({ ab: b instanceof ArrayBuffer, len: b.byteLength }); }
  if (q === "blob") { const b = await request.blob(); return Response.json({ size: b.size, type: b.type, text: await b.text() }); }
  if (q === "bytes") { const b = await request.bytes(); return Response.json({ u8: b instanceof Uint8Array, len: b.length }); }
  return Response.json({ text: await request.text() });
}
