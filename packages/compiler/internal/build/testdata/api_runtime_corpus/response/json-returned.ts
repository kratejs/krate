export async function GET() { return Response.json({ ok: true, n: 1.5, s: "é" }, { status: 203, headers: { "X-Custom": "c" } }); }
