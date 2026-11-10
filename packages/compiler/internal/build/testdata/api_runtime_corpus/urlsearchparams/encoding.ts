export async function GET() {
  const p = new URLSearchParams();
  for (const v of ["a b", "a+b", "~", "*-._", "!'()", "é", "\ud800", "&=", "/?#", "\u0000", "%"]) p.append("k", v);
  const parsed = new URLSearchParams("a=a+b&b=%2B&c=%zz&d=%E2%82&e=%e2%82%ac&f==x&g&=h&&i=1%202");
  return Response.json({ serialized: p.toString(), parsed: [...parsed], ctor: new URLSearchParams({ "a b": "c+d" }).toString() });
}
