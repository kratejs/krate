export async function GET(request) {
  const u = new URL(request.url);
  let bad = "no";
  try { new URL("not a url"); } catch (e) { bad = e.name; }
  return Response.json({ origin: u.origin, host: u.host, path: u.pathname, e: u.searchParams.get("e"), p: u.searchParams.get("p"), all: u.searchParams.getAll("k"), bad });
}
