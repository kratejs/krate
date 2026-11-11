export async function GET() { return Response.json([new URL("blob:https://a.example/uuid").origin, new URL("blob:foo://x/y").origin]); }
