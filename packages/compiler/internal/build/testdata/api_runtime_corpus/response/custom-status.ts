export async function GET() { return new Response("short and stout", { status: 418, statusText: "I'm a teapot", headers: { "content-type": "text/plain" } }); }
export async function POST() { return new Response(null, { status: 204 }); }
export async function PUT() { return new Response("gone", { status: 410 }); }
