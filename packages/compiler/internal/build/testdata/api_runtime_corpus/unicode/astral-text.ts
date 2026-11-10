export async function POST(request) {
  const t = await request.text();
  return new Response(JSON.stringify({ len: t.length, cps: Array.from(t).length, rev: Array.from(t).reverse().join("") }) + "\ud83d\ude80", { headers: { "content-type": "text/plain; charset=utf-8" } });
}
