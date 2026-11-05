import { cookies } from "@krate/runtime/server";

// Per-user: reading cookies keeps the page out of every cache.
export default async function Me() {
  const jar = await cookies();
  return <p id="who">Hello {jar.get("sid")?.value ?? "anon"}</p>;
}
