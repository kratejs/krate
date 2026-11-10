export async function GET() {
  const euro = [0xe2, 0x82, 0xac], smile = [0xf0, 0x9f, 0x98, 0x80];
  const run = (chunks: number[][], fatal = false) => {
    const t = new TextDecoder("utf-8", { fatal });
    const parts: string[] = [];
    try {
      for (const c of chunks) parts.push(t.decode(new Uint8Array(c), { stream: true }));
      parts.push(t.decode());
    } catch (e) { parts.push("throws:" + (e instanceof TypeError)); }
    return parts.map((p) => Array.from(p, (c) => c.codePointAt(0)!.toString(16)).join(" "));
  };
  return Response.json({
    euroSplit: run([[0x41, euro[0]], [euro[1]], [euro[2], 0x42]]),
    smileByByte: run(smile.map((b) => [b])),
    bomSplit: run([[0xef], [0xbb, 0xbf, 0x41]]),
    danglingAtEnd: run([[0x41, 0xe2, 0x82]]),
    danglingFatal: run([[0x41, 0xe2, 0x82]], true),
    emptyChunks: run([[], [0x41], []]),
    utf16Split: (() => { const t = new TextDecoder("utf-16le"); return [t.decode(new Uint8Array([0x41]), { stream: true }), t.decode(new Uint8Array([0x00, 0x42, 0x00]))]; })(),
  });
}
