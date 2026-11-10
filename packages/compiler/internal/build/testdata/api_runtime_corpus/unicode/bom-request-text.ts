export async function POST(request) {
  const t = await request.text();
  return Response.json({ first: t.charCodeAt(0).toString(16), len: t.length });
}
