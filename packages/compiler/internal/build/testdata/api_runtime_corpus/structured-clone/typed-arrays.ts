export async function GET() {
  const buf = new ArrayBuffer(8);
  const u8 = new Uint8Array(buf); u8.set([1, 2, 3, 4, 5, 6, 7, 8]);
  const view = new Uint16Array(buf, 2, 2);
  const c = structuredClone({ u8, view, f64: new Float64Array([1.5, -0, NaN]), dv: new DataView(buf, 4), buf, i32: new Int32Array([-1, 2 ** 31 - 1]), big: new BigInt64Array([-5n]) });
  return Response.json({
    u8: Array.from(c.u8), viewOff: [c.view.byteOffset, c.view.length, Array.from(c.view)],
    sharedBuf: c.u8.buffer === c.view.buffer && c.buf === c.u8.buffer && c.dv.buffer === c.buf,
    detachedFromOrig: c.buf !== buf,
    f64: Array.from(c.f64).map((x) => Object.is(x, -0) ? "-0" : String(x)),
    dv: [c.dv.byteOffset, c.dv.byteLength, c.dv.getUint8(0)],
    i32: Array.from(c.i32), big: String(c.big[0]),
    types: [c.u8.constructor.name, c.view.constructor.name, c.dv.constructor.name, c.big.constructor.name],
  });
}
