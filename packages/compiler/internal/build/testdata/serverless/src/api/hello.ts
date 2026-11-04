export async function GET(req: Request) {
  const url = new URL(req.url);
  return Response.json({ hello: url.searchParams.get("name") ?? "world" });
}
export async function POST(req: Request) {
  const body = await req.text();
  return new Response(JSON.stringify({ len: body.length }), { status: 201, headers: { "content-type": "application/json" } });
}
