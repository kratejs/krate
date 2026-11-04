"use server";

import { redirect } from "@krate/runtime/server";

export async function goAbout(_formData: FormData) {
  redirect("/about");
}

export async function login(_prev: { error: string }, formData: FormData) {
  if (String(formData.get("user") || "") === "") return { error: "user is required" };
  redirect("/account");
}
