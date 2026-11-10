export async function GET() {
  const base = new Headers({ a: "1" });
  const copy = new Headers(base); copy.set("a", "2");
  return Response.json({
    obj: [...new Headers({ B: "2", a: "1" })], arr: [...new Headers([["x", "1"], ["X", "2"]])], map: [...new Headers(new Map([["m", "v"]]))],
    copyIndependent: [base.get("a"), copy.get("a")], undef: [...new Headers(undefined)],
  });
}
