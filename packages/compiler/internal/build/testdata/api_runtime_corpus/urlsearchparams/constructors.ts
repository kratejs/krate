export async function GET() {
  const out: Record<string, unknown> = {
    obj: new URLSearchParams({ a: "1", b: 2 as any }).toString(),
    arr: new URLSearchParams([["a", "1"], ["a", "2"]]).toString(),
    str: new URLSearchParams("?x=1&y=2").toString(),
    strNoQ: new URLSearchParams("x=1").toString(),
    copy: new URLSearchParams(new URLSearchParams("q=1")).toString(),
    map: new URLSearchParams(new Map([["m", "1"]])).toString(),
    empty: new URLSearchParams().toString(),
    undef: new URLSearchParams(undefined).toString(),
  };
  try { new URLSearchParams([["a"]] as any); out.badPair = "ok"; } catch (e) { out.badPair = e instanceof TypeError; }
  return Response.json(out);
}
