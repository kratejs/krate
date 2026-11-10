export async function GET() {
  const p = new URLSearchParams("c=1&a=2&b=3&a=1&B=0");
  p.sort();
  const q = new URLSearchParams("\ufb03=1&\ud83c\udf08=2&a=3&\uff41=4");
  q.sort();
  return Response.json({ p: p.toString(), q: [...q.keys()] });
}
