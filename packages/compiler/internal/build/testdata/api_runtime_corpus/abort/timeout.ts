export async function GET() {
  const s = AbortSignal.timeout(10);
  const immediate = s.aborted;
  const ev = await new Promise<string>((res) => s.addEventListener("abort", (e) => res(e.type)));
  const r: any = s.reason;
  return Response.json({ immediate, ev, aborted: s.aborted, reason: [r instanceof DOMException, r.name, r.code] });
}
