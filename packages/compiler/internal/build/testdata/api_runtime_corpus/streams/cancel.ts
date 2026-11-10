export async function GET() {
  const log: string[] = [];
  const s = new ReadableStream({ start(c) { c.enqueue("a"); }, cancel(reason) { log.push("cancel:" + reason); } });
  await s.cancel("why");
  const r = s.getReader();
  const x = await r.read();
  const read = [x.value ?? null, x.done];
  const s2 = new ReadableStream({ cancel(reason) { log.push("reader-cancel:" + reason); } });
  const r2 = s2.getReader();
  await r2.cancel("via reader");
  const s3 = new ReadableStream(); s3.getReader();
  const rejected = await s3.cancel().then(() => "resolved", (e) => e instanceof TypeError);
  return Response.json({ log, read, rejected });
}
