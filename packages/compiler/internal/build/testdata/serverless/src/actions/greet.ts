"use server";
import { redirect, forbidden, unauthorized } from "@krate/runtime/server";

export async function greet(formData: FormData) {
  const who = String(formData.get("who") || "");
  if (who === "") return { error: "who is required" };
  redirect("/about");
}

export async function add(a: number, b: number) {
  return { sum: a + b };
}

export async function secret() {
  forbidden();
}

export async function signin() {
  unauthorized();
}
