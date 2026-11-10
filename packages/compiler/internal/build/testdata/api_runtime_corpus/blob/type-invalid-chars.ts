export async function GET() {
  return Response.json(["t\u00e9xt/plain", "text/\u0001", "text/\u007f"].map((t) => new Blob([], { type: t }).type));
}
