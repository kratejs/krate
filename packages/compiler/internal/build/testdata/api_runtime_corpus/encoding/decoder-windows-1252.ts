export async function GET() {
  const bytes = new Uint8Array([0x41, 0x80, 0x8a, 0x9f, 0xa0, 0xe9, 0xff]);
  return Response.json(Array.from(new TextDecoder("latin1").decode(bytes), (c) => c.codePointAt(0)!.toString(16)));
}
