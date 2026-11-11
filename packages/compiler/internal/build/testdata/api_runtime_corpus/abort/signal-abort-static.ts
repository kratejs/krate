export async function GET() {
  const s = AbortSignal.abort();
  const t = AbortSignal.abort({ why: 1 });
  return Response.json({ aborted: s.aborted, name: (s.reason as any).name, custom: t.reason, isSignal: s instanceof AbortSignal && s instanceof EventTarget });
}
