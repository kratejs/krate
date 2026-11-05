import { forbidden } from "@krate/runtime/server";
export async function GET() {
  forbidden();
}
