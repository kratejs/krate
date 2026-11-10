export async function GET() {
  return Response.json({ v: process.env.KRATE_CORPUS_VAR ?? null, missing: process.env.KRATE_CORPUS_MISSING ?? null, type: typeof process.env, inOp: "KRATE_CORPUS_VAR" in process.env });
}
