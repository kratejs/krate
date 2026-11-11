export async function GET() {
  const sparse = [1, , 3];
  const c: any = structuredClone({ u: undefined, n: null, nan: NaN, negZero: -0, inf: -Infinity, big: 10n ** 20n, b: new Boolean(false), s: new String("str"), num: new Number(7), sparse });
  return Response.json({
    hasU: "u" in c, u: c.u === undefined, n: c.n, nan: Number.isNaN(c.nan), negZero: Object.is(c.negZero, -0), inf: String(c.inf), big: String(c.big),
    b: [typeof c.b, c.b.valueOf()], s: [typeof c.s, c.s.valueOf()], num: [typeof c.num, c.num.valueOf()],
    sparse: [c.sparse.length, 1 in c.sparse],
    top: [structuredClone(5), structuredClone("x"), structuredClone(null), structuredClone(undefined) === undefined],
  });
}
