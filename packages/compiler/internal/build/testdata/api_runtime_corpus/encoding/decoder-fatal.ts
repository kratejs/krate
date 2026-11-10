const seqs: Record<string, number[]> = {
  overlong: [0xc0, 0x80], overlong3: [0xe0, 0x80, 0x80], surrogate: [0xed, 0xa0, 0x80], tooBig: [0xf4, 0x90, 0x80, 0x80],
  truncated: [0xe2, 0x82], truncatedMid: [0xe2, 0x82, 0x41], loneCont: [0x80], ff: [0xff], fe: [0xfe], f8: [0xf8, 0x88, 0x80, 0x80, 0x80],
  mixed: [0x41, 0xc3, 0x28, 0x42], valid: [0xe2, 0x82, 0xac],
};
export async function GET() {
  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(seqs)) {
    const lossy = new TextDecoder().decode(new Uint8Array(v));
    let fatal: string;
    try { fatal = JSON.stringify(new TextDecoder("utf-8", { fatal: true }).decode(new Uint8Array(v))); } catch (e) { fatal = e instanceof TypeError ? "TypeError" : "other:" + e.name; }
    out[k] = [Array.from(lossy, (c) => c.codePointAt(0)!.toString(16)), fatal];
  }
  return Response.json(out);
}
