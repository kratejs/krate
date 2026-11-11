export async function GET() {
  const a = performance.now();
  await new Promise((r) => setTimeout(r, 5));
  const b = performance.now();
  return Response.json({ type: typeof a, monotonic: b >= a, elapsed: b - a >= 3, origin: typeof performance.timeOrigin, originSane: Math.abs(performance.timeOrigin + b - Date.now()) < 1000 });
}
