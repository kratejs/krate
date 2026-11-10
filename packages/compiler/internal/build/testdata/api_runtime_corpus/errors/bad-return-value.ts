export async function GET(request) {
  const k = new URL(request.url).searchParams.get("k");
  if (k === "string") return "plain string" as any;
  if (k === "number") return 5 as any;
  return [1, "two"] as any;
}
