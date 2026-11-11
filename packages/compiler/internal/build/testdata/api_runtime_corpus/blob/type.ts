export async function GET() {
  const types = ["text/plain", "TEXT/Plain", "text/plain;charset=UTF-8", "", " padded "];
  return Response.json(types.map((t) => new Blob([], { type: t }).type));
}
