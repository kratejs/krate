export async function GET() {
  const u = new URL("http://x.example/a?b#c");
  return Response.json({ json: JSON.stringify({ u }), str: String(u), toStr: u.toString(), tmpl: `${u}`, keys: Object.keys(u), toJSON: u.toJSON() });
}
