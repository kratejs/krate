export async function POST(request) {
  const c = request.clone();
  const a = await request.text();
  const b = await c.text();
  return Response.json({ a, b, used: [request.bodyUsed, c.bodyUsed], url: c.url === request.url, method: c.method, h: c.headers.get("x-test") });
}
