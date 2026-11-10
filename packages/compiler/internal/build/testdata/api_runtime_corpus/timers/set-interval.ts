export async function GET() {
  let n = 0;
  const done = await new Promise<number>((res) => { const id = setInterval(() => { n++; if (n === 3) { clearInterval(id); setTimeout(() => res(n), 30); } }, 5); });
  return Response.json({ n: done });
}
