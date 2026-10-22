"use server";

// Server actions for the React 19 server fixtures. Under Krate these run in
// the Node sidecar behind POST /__krate/action/<id>; the React oracle bundles
// the same module and runs it as a plain async function in the browser.

export async function signGuestbook(
  prev: { entries: string[]; error: string },
  formData: FormData,
): Promise<{ entries: string[]; error: string }> {
  await new Promise((r) => setTimeout(r, 300));
  const name = String(formData.get("name") || "").trim();
  if (!name) return { entries: prev.entries, error: "name is required" };
  return { entries: [...prev.entries, name], error: "" };
}

export async function lookupUser(id: number): Promise<{ id: number; name: string }> {
  await new Promise((r) => setTimeout(r, 300));
  const names = ["root", "ada", "grace", "alan"];
  return { id, name: names[id] ?? "unknown" };
}
