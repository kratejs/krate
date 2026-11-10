export async function GET() {
  return Response.json({ sorted: ["b", "a", "\u00e4", "B"].sort((a, b) => a.localeCompare(b)), intl: typeof Intl, num: (1234.5).toLocaleString("en-US") });
}
