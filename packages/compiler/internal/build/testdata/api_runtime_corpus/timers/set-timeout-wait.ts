export async function GET() {
  const t0 = Date.now();
  await new Promise((r) => setTimeout(r, 25));
  return Response.json({ waited: Date.now() - t0 >= 20 });
}
