export async function GET(_req: Request, ctx: { params: { id: string } }) {
  return Response.json({ id: ctx.params.id });
}
