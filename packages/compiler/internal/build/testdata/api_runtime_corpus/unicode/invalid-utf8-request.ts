export async function POST(request) {
  const t = await request.text();
  return Response.json({ cps: Array.from(t, (c) => c.codePointAt(0)!.toString(16)) });
}
