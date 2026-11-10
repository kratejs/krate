export async function GET() {
  const p = new URLSearchParams("a=1&b=2&a=3&a=4&c=");
  const out: Record<string, unknown> = { get: p.get("a"), all: p.getAll("a"), size: p.size, empty: p.get("c"), missing: p.get("z"), missingAll: p.getAll("z") };
  out.hasValue = [p.has("a", "3"), p.has("a", "9")];
  p.delete("a", "3"); out.afterDeleteValue = p.toString();
  p.set("a", "x"); out.afterSet = p.toString();
  p.delete("a"); out.afterDelete = p.toString();
  return Response.json(out);
}
