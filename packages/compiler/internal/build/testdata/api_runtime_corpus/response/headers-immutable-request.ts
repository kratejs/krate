export async function GET(request) {
  let mutable = "ok";
  try { request.headers.set("x-new", "1"); } catch (e) { mutable = e instanceof TypeError ? "TypeError" : e.name; }
  return Response.json({ mutable });
}
