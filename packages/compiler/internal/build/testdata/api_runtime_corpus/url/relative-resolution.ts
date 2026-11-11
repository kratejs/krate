const base = "http://a/b/c/d;p?q";
const inputs = ["g:h", "g", "./g", "g/", "/g", "//g", "?y", "g?y", "#s", "g#s", "g?y#s", ";x", "g;x", "g;x?y#s", "", ".", "./", "..", "../", "../g", "../..", "../../", "../../g", "../../../g", "../../../../g", "/./g", "/../g", "g.", ".g", "g..", "..g", "./../g", "./g/.", "g/./h", "g/../h", "g;x=1/./y", "g;x=1/../y", "g?y/./x", "g?y/../x", "g#s/./x", "g#s/../x", "http:g"];
export async function GET() {
  const out: Record<string, string> = {};
  for (const i of inputs) out[i] = new URL(i, base).href;
  out["base-object"] = new URL("x/y", new URL("https://h.example/p/q")).href;
  out["scheme-relative-https"] = new URL("//other.example/z", "https://h.example/a").href;
  out["file-base"] = new URL("../x", "file:///a/b/c").href;
  return Response.json(out);
}
