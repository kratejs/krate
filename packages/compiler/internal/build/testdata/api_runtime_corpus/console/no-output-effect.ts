export async function GET() {
  const cyc: any = { a: 1 }; cyc.self = cyc;
  console.log("log", 1, { a: [1, 2] }, null, undefined);
  console.info("info"); console.warn("warn", new Error("not thrown")); console.error("error", cyc); console.debug("debug", 1n, Symbol("s"), () => 1);
  return Response.json({ types: ["log", "info", "warn", "error", "debug", "trace"].map((m) => typeof (console as any)[m]), result: console.log("x") });
}
