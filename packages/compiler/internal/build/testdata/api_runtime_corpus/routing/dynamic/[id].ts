export async function GET(request, ctx) { return Response.json({ params: ctx.params, path: new URL(request.url).pathname }); }
