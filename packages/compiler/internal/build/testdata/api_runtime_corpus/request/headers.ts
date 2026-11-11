export async function GET(request) {
  return Response.json({ auth: request.headers.get("authorization"), host: request.headers.get("host"), has: request.headers.has("AUTHORIZATION"), cookie: request.cookies.get("sid")?.value ?? null });
}
