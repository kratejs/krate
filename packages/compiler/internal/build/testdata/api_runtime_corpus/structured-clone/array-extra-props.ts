export async function GET() {
  const a: any = [1, 2]; a.extra = "x";
  const c: any = structuredClone(a);
  return Response.json({ len: c.length, extra: c.extra ?? null });
}
