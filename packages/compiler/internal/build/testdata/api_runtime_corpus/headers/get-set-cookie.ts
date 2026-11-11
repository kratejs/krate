export async function GET() {
  const h = new Headers([["Set-Cookie", "a=1; Expires=Wed, 21 Oct 2015 07:28:00 GMT"], ["set-cookie", "b=2"]]);
  const none = new Headers().getSetCookie();
  return Response.json({ list: h.getSetCookie(), joined: h.get("set-cookie"), none, copy: new Headers(h).getSetCookie() });
}
