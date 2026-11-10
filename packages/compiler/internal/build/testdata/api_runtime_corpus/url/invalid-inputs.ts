const inputs = ["", "not a url", "http://[::1/", "http://exa mple.com/", "http://a:b:c/", "http://:80/", "https://", "http://ex%41mple.com/", "//no-scheme", "http://example.com:99999/", "http://ex<ample.com/"];
export async function GET() {
  const out: Record<string, unknown> = {};
  for (const i of inputs) {
    try { out[JSON.stringify(i)] = new URL(i).href; } catch (e) { out[JSON.stringify(i)] = "throws:" + e.name + ":" + (e instanceof TypeError); }
  }
  return Response.json(out);
}
