export async function GET() {
  const bom = [0xef, 0xbb, 0xbf];
  const d = (bytes: number[], opts?: any, label = "utf-8") => JSON.stringify(new TextDecoder(label, opts).decode(new Uint8Array(bytes)));
  return Response.json({
    stripped: d([...bom, 0x41]),
    kept: d([...bom, 0x41], { ignoreBOM: true }),
    middle: d([0x41, ...bom, 0x42]),
    double: d([...bom, ...bom, 0x41]),
    onlyBom: d(bom),
    utf16: d([0xff, 0xfe, 0x41, 0x00], undefined, "utf-16le"),
    utf16Kept: d([0xff, 0xfe, 0x41, 0x00], { ignoreBOM: true }, "utf-16le"),
    reuse: (() => { const t = new TextDecoder(); return [t.decode(new Uint8Array([...bom, 0x31])), t.decode(new Uint8Array([...bom, 0x32]))]; })(),
    flags: (() => { const t = new TextDecoder("utf-8", { ignoreBOM: true, fatal: true }); return [t.ignoreBOM, t.fatal, t.encoding]; })(),
  });
}
