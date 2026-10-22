"use server";

import { headers } from "@krate/runtime/server";

export async function saveFeedback(input: FormData | { text: string }) {
  const text =
    input instanceof FormData ? String(input.get("text") || "") : String(input.text || "");
  return {
    text: text.trim(),
    length: text.trim().length,
    via: headers().get("x-krate-action") === "fetch" ? "fetch" : "form",
    savedAt: new Date().toISOString(),
  };
}

// useActionState action: receives the previous state, returns the next one.
export async function addTodo(prev: string[], formData: FormData) {
  const title = String(formData.get("title") || "").trim();
  await new Promise((resolve) => setTimeout(resolve, 150));
  if (!title) return prev;
  return [...(Array.isArray(prev) ? prev : []), title];
}

// A deliberately slow echo so optimistic UI is observable.
export async function sendMessage(formData: FormData) {
  await new Promise((resolve) => setTimeout(resolve, 400));
  return { text: String(formData.get("message") || "").trim() };
}
