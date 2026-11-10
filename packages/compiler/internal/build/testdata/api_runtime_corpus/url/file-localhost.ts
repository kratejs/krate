export async function GET() { return Response.json([new URL("file://localhost/a").href, new URL("file://LOCALHOST/b").host]); }
