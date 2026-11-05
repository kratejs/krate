import { unauthorized, notFound } from "@krate/runtime/server";
export async function GET(req: Request) {
  if (new URL(req.url).searchParams.has("missing")) notFound();
  unauthorized();
}
