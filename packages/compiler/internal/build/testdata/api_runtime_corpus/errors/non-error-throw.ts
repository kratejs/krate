export async function GET(request) {
  const k = new URL(request.url).searchParams.get("k");
  if (k === "string") throw "a string";
  if (k === "object") throw { code: 1 };
  if (k === "null") throw null;
  if (k === "undefined") throw undefined;
  if (k === "number") throw 42;
  return new Response("unreachable");
}
