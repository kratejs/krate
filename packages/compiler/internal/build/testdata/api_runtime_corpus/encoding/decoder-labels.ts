export async function GET() {
  const out: Record<string, unknown> = {};
  for (const l of ["utf8", "UTF-8", " utf-8 ", "unicode-1-1-utf-8", "utf-16le", "utf-16", "latin1", "iso-8859-1", "ascii", "us-ascii", "windows-1252"]) {
    try { out[l] = new TextDecoder(l).encoding; } catch (e) { out[l] = "throws:" + e.name; }
  }
  for (const l of ["utf-32", "bogus", ""]) {
    try { out[l] = new TextDecoder(l).encoding; } catch (e) { out[l] = "throws:" + (e instanceof RangeError); }
  }
  return Response.json(out);
}
