export async function GET() {
  const s = new ReadableStream();
  return Response.json({ tee: typeof s.tee, pipeTo: typeof s.pipeTo, pipeThrough: typeof s.pipeThrough, writable: typeof WritableStream, transform: typeof TransformStream });
}
