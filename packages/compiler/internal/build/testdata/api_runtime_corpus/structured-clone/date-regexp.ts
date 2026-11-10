export async function GET() {
  const r = /a+b/gi; r.lastIndex = 3;
  const c = structuredClone({ d: new Date(1700000000123), bad: new Date(NaN), r });
  return Response.json({ d: c.d.toISOString(), isDate: c.d instanceof Date, bad: String(c.bad.getTime()), src: c.r.source, flags: c.r.flags, lastIndex: c.r.lastIndex, isRe: c.r instanceof RegExp });
}
