"use server";

// Server actions for the React 19 server fixtures. Under Krate these run in
// the Node sidecar behind POST /__krate/action/<id>; the React oracle bundles
// the same module and runs it as a plain async function in the browser.

// Long enough that the snapshot right after a submit always sees the action
// pending on both sides, even on a loaded runner (see action_use_action_state).
const ACTION_DELAY_MS = 1500;

export async function signGuestbook(
  prev: { entries: string[]; error: string },
  formData: FormData,
): Promise<{ entries: string[]; error: string }> {
  await new Promise((r) => setTimeout(r, ACTION_DELAY_MS));
  const name = String(formData.get("name") || "").trim();
  if (!name) return { entries: prev.entries, error: "name is required" };
  return { entries: [...prev.entries, name], error: "" };
}

export async function lookupUser(id: number): Promise<{ id: number; name: string }> {
  await new Promise((r) => setTimeout(r, ACTION_DELAY_MS));
  const names = ["root", "ada", "grace", "alan"];
  return { id, name: names[id] ?? "unknown" };
}
