export async function GET() {
  let json: unknown;
  try { json = await new Response(new Uint8Array([0xef, 0xbb, 0xbf, 0x7b, 0x7d])).json(); } catch (e) { json = "throws:" + e.name; }
  const text = await new Response(new Uint8Array([0xef, 0xbb, 0xbf, 0x41])).text();
  const blob = await new Blob([new Uint8Array([0xef, 0xbb, 0xbf, 0x42])]).text();
  return Response.json({ json, text: text.length, blob: blob.length });
}
