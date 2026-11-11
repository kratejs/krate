export async function POST(request) {
  const body = await request.json().catch(() => null);
  if (!body) return new Response(JSON.stringify({ error: "bad json" }), { status: 400, headers: { "content-type": "application/json" } });
  return Response.json({ got: body });
}
