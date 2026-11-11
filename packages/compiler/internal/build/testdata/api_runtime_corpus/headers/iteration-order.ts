export async function GET() {
  const h = new Headers();
  h.append("zeta", "1"); h.append("Alpha", "2"); h.append("beta", "b1"); h.append("Beta", "b2");
  h.append("set-cookie", "c=1"); h.append("Set-Cookie", "d=2"); h.append("accept", "x");
  const fe: string[] = [];
  h.forEach((v, k) => fe.push(k + "=" + v));
  return Response.json({ entries: [...h], keys: [...h.keys()], values: [...h.values()], fe });
}
