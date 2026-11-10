export async function GET() {
  const e: any = new Error("msg", { cause: "why" });
  const t = new TypeError("t");
  class MyErr extends Error { constructor() { super("mine"); this.name = "MyErr"; } }
  const m = new MyErr();
  let caught: any;
  try { (null as any).x; } catch (x) { caught = x; }
  return Response.json({
    e: [e.name, e.message, e.cause, String(e), typeof e.stack, e instanceof Error], t: [t.name, String(t), t instanceof Error],
    m: [m.name, String(m), m instanceof MyErr, m instanceof Error], caught: [caught.constructor.name, caught instanceof TypeError],
    dom: (() => { const d = new DOMException("dm", "NotFoundError"); return [d.name, d.message, d.code, d instanceof Error, String(d)]; })(),
    domDefault: (() => { const d = new DOMException(); return [d.name, d.message, d.code]; })(),
    aggregate: (() => { const a = new AggregateError([new Error("x")], "agg"); return [a.name, a.errors.length, a.message]; })(),
  });
}
