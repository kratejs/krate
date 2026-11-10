export async function GET() {
  const re = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
  const ids = Array.from({ length: 50 }, () => crypto.randomUUID());
  return Response.json({ allMatch: ids.every((x) => re.test(x)), unique: new Set(ids).size, len: ids[0].length, type: typeof ids[0] });
}
