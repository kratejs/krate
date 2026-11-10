class Point { x = 1; get len() { return 5; } }
export async function GET() {
  const g = { get v() { return 42; } };
  const p = structuredClone(new Point());
  const sym = Symbol("k");
  const withSym: any = { a: 1 }; withSym[sym] = 2;
  const nonEnum = Object.defineProperty({ a: 1 }, "hidden", { value: 2, enumerable: false });
  return Response.json({
    classProto: Object.getPrototypeOf(p) === Object.prototype, classKeys: Object.keys(p),
    getter: Object.getOwnPropertyDescriptor(structuredClone(g), "v"),
    symKeys: Object.getOwnPropertySymbols(structuredClone(withSym)).length,
    nonEnum: Object.keys(structuredClone(nonEnum)),
  });
}
