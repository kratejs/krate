export async function GET() {
  let code = null, name = null, isType = false;
  try { new URL("::"); } catch (e) { code = e.code ?? null; name = e.name; isType = e instanceof TypeError; }
  return Response.json({
    canParse: [URL.canParse("http://x"), URL.canParse("nope"), URL.canParse("/p", "http://x"), URL.canParse("/p")],
    parse: [URL.parse("http://x/a")?.href ?? null, URL.parse("nope"), URL.parse("b", "http://x/a/")?.href ?? null],
    code, name, isType,
    noArgs: (() => { try { new (URL as any)(); return "ok"; } catch (e) { return e instanceof TypeError; } })(),
  });
}
