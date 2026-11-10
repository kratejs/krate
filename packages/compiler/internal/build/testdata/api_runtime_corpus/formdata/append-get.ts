export async function GET() {
  const f = new FormData();
  f.append("a", "1"); f.append("a", "2"); f.append("b", 3 as any); f.append("c", "");
  const before = { get: f.get("a"), all: f.getAll("a"), b: f.get("b"), c: f.get("c"), missing: f.get("z"), missingAll: f.getAll("z"), has: [f.has("a"), f.has("z")] };
  f.set("a", "x"); const afterSet = [...f];
  f.delete("b"); const afterDelete = [...f];
  const fe: string[] = []; f.forEach((v, k) => fe.push(k + "=" + v));
  return Response.json({ before, afterSet, afterDelete, keys: [...f.keys()], values: [...f.values()], fe });
}
