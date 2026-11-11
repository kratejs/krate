export async function GET() {
  return Response.json({ high: "\ud800", low: "\udc00", pair: "\ud83d\ude00", reversed: "\ude00\ud83d", ctrl: "\u0001\u001f", sep: "\u2028\u2029" });
}
