export async function GET(request) {
  return Response.json({
    method: request.method, url: request.url, bodyNull: request.body === null, bodyUsed: request.bodyUsed,
    signal: [request.signal instanceof AbortSignal, request.signal.aborted], isRequest: request instanceof Request,
    redirect: request.redirect, ua: request.headers.get("user-agent"),
  });
}
export async function POST(request) {
  return Response.json({ method: request.method, hasBody: request.body !== null, bodyUsed: request.bodyUsed, ct: request.headers.get("content-type") });
}
