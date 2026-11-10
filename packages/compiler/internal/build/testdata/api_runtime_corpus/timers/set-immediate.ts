export async function GET() {
  const log: string[] = [];
  const v = await new Promise<string>((res) => setImmediate((x: string) => res(x), "imm"));
  const id = setImmediate(() => log.push("cleared"));
  clearImmediate(id);
  await new Promise((r) => setTimeout(r, 5));
  return Response.json({ v, log });
}
