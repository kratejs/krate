export async function POST(request) {
  const f = await request.formData();
  const file = f.get("file");
  return Response.json({ title: f.get("title"), name: file.name, type: file.type, text: await file.text(), size: file.size });
}
