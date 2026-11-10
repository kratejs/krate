export async function POST(request) {
  const b = new Uint8Array(await request.arrayBuffer());
  return Response.json({ len: b.length, ok: b.every((x, i) => x === i), text: Array.from(await new Response(b).text()).length });
}
