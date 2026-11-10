export async function GET() {
  const p = new URLSearchParams("b=2&a=1&b=3");
  const fe: string[] = [];
  p.forEach((v, k) => fe.push(k + ":" + v));
  return Response.json({ keys: [...p.keys()], values: [...p.values()], entries: [...p.entries()], spread: [...p], fe, fromEntries: Object.fromEntries(p), str: String(p) });
}
