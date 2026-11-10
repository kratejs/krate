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
  const t = new EventTarget();
  const e = new Event("go", { cancelable: true, bubbles: true });
  const before = [e.type, e.cancelable, e.bubbles, e.defaultPrevented, e.target, typeof e.timeStamp];
  let seen: unknown[] = [];
  t.addEventListener("go", (ev) => { ev.preventDefault(); seen = [ev.target === t, ev.currentTarget === t]; });
  const ret = t.dispatchEvent(e);
  const plain = new Event("p");
  plain.preventDefault();
  return Response.json({ before, seen, ret, prevented: e.defaultPrevented, plain: [plain.defaultPrevented, new EventTarget().dispatchEvent(plain)] });
}
