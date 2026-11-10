export async function GET() {
  const h = new Headers({ "Content-Type": "text/html" });
  h.append("X-MiXeD", "1");
  return Response.json({ get: [h.get("content-type"), h.get("CONTENT-TYPE"), h.get("x-mixed")], has: [h.has("X-Mixed"), h.has("nope")], keys: [...h.keys()], deleted: (h.delete("CONTENT-type"), h.has("content-type")), missing: h.get("missing") });
}
