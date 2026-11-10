export async function GET() {
  await new Promise((r) => setTimeout(r, 2));
  await null;
  throw new TypeError("late failure");
}
