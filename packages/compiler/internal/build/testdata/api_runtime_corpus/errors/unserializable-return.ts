export async function GET(request) {
  if (new URL(request.url).searchParams.get("k") === "fn") return (() => 1) as any;
  return Symbol("x") as any;
}
