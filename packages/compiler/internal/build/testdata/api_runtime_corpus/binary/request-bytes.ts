export async function POST(request) {
  const buf = new Uint8Array(await request.arrayBuffer());
  return Response.json({ len: buf.length, hex: Array.from(buf).map((b) => b.toString(16).padStart(2, "0")).join("") });
}
