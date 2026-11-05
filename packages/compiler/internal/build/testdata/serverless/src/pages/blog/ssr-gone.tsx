import { notFound } from "@krate/runtime/server";
export const config = { ssr: true };
export default function Gone() {
  notFound();
  return <p>never</p>;
}
