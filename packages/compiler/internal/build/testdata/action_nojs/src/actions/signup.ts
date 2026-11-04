"use server";

import { redirect } from "@krate/runtime/server";

export async function signup(_prev: { error: string }, formData: FormData) {
  if (String(formData.get("user") || "") === "") return { error: "user is required" };
  redirect("/done");
}
