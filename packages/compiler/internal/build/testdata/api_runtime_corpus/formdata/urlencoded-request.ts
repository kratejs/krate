export async function POST(request) {
  const f = await request.formData();
  return Response.json({ a: f.getAll("a"), b: f.get("b") });
}
