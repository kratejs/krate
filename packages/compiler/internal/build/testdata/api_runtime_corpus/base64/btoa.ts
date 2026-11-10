export async function GET() {
  const out: Record<string, unknown> = {};
  for (const s of ["", "f", "fo", "foo", "foob", "fooba", "foobar", "ÿþ\u0000", "hello world"]) out[JSON.stringify(s)] = btoa(s);
  out.number = btoa(12 as any);
  out.nul = btoa(null as any);
  return Response.json(out);
}
