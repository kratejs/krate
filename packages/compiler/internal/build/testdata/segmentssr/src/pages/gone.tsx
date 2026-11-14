import { notFound } from "@krate/runtime";

export const config = { ssr: true };

export default async function Gone() {
  await Promise.resolve();
  notFound();
}
