import { revalidateTag } from "@krate/runtime/cache";

export async function POST() {
  revalidateTag("items");
  return Response.json({ ok: true });
}
