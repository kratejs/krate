export async function GET() {
  const b = new Blob(["0123456789"], { type: "text/plain" });
  const t = async (x: Blob) => [await x.text(), x.size, x.type];
  return Response.json({
    mid: await t(b.slice(2, 5)), neg: await t(b.slice(-3)), negEnd: await t(b.slice(1, -1)), over: await t(b.slice(5, 100)),
    inverted: await t(b.slice(6, 2)), typed: await t(b.slice(0, 1, "X/Custom")), none: await t(b.slice()), nested: await t(b.slice(2).slice(1, 3)),
  });
}
