export async function POST(request) {
  const r = request.body.getReader();
  while (!(await r.read()).done) {}
  const resp = new Response("x");
  const rr = resp.body.getReader();
  while (!(await rr.read()).done) {}
  return Response.json({ used: [request.bodyUsed, resp.bodyUsed] });
}
