import { cookies, forbidden } from "@krate/runtime/server";

// An async server component runs on every request, so it can gate the whole page.
export async function Gate() {
  const jar = await cookies();
  if (jar.get("role")?.value === "banned") forbidden();
  return <i id="gate-ok" />;
}
