"use server";
import { revalidateTag } from "next/cache";

export async function addPost(title: string) {
  await fetch(`${process.env.DATA_URL}/api/posts`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ title }),
  });
  revalidateTag("posts");
}
