export async function GET() {
  const out: Record<string, unknown> = {};
  const custom = new Error("c"); custom.name = "CustomError";
  for (const e of [new Error("m"), new TypeError("t"), new RangeError("r"), new SyntaxError("s"), new EvalError("e"), new URIError("u"), new ReferenceError("f"), custom, new Error("withCause", { cause: { x: 1 } })]) {
    const c: any = structuredClone(e);
    out[e.name + ":" + e.message] = [c.constructor.name, c.name, c.message, c instanceof Error, c === e, c.cause ?? null, typeof c.stack];
  }
  return Response.json(out);
}
