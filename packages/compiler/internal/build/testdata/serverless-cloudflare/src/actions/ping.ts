"use server";
import { getCloudflareContext } from "@krate/runtime/server";

export async function ping() {
  return { db: String(getCloudflareContext().env.DB) };
}
