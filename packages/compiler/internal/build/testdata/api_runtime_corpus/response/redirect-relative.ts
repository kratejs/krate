export async function GET(request) { return Response.redirect(new URL("/login", request.url), 307); }
