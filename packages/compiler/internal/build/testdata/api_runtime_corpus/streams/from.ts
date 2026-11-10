export async function GET() {
  if (typeof (ReadableStream as any).from !== "function") return Response.json({ missing: true });
  async function* gen() { yield "a"; yield "b"; }
  const got: string[] = [];
  for await (const x of (ReadableStream as any).from(gen())) got.push(x);
  for await (const x of (ReadableStream as any).from(["c", "d"])) got.push(x);
  return Response.json({ got });
}
