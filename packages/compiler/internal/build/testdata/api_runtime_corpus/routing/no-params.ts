export async function GET(request, ctx) { return Response.json({ id: ctx.params.id ?? null, keys: Object.keys(ctx.params) }); }
