export async function GET() { return Response.json({ ok: true }); }
export async function POST() { return new Response(null, { status: 201 }); }
