export async function POST(request) {
  const f = await request.formData();
  const out: unknown[] = [];
  for (const [k, v] of f) out.push(typeof v === "string" ? [k, v] : [k, v.name, v.type, v.size, await v.text()]);
  return Response.json(out);
}
