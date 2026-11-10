function kind(e: any): string {
  if (typeof DOMException === "function" && e instanceof DOMException) return "DOMException:" + e.name;
  if (e instanceof TypeError) return "TypeError";
  if (e instanceof RangeError) return "RangeError";
  if (e instanceof SyntaxError) return "SyntaxError";
  if (e instanceof Error) return "Error:" + e.name;
  return "thrown:" + typeof e;
}
function errOf(fn: () => unknown): string {
  try { fn(); return "ok"; } catch (e) { return kind(e); }
}
async function errOfAsync(fn: () => unknown): Promise<string> {
  try { await fn(); return "ok"; } catch (e) { return kind(e); }
}
export async function GET() {
  const got = await new Promise<unknown[]>((res) => setTimeout((a: number, b: string) => res([a, b]), 1, 7, "x"));
  const h: any = setTimeout(() => {}, 1000);
  const info = [typeof h, typeof +h, typeof h.ref, typeof h.unref, typeof h.hasRef];
  clearTimeout(h);
  const byNumber: string[] = [];
  const h2: any = setTimeout(() => byNumber.push("fired"), 5);
  clearTimeout(+h2);
  await new Promise((r) => setTimeout(r, 15));
  return Response.json({ got, info, byNumber, badCb: errOf(() => setTimeout("code" as any, 1)) });
}
