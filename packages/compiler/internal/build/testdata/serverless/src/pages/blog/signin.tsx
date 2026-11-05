import { unauthorized } from "@krate/runtime/server";
export const config = { ssr: true };
export default function SignIn() {
  unauthorized();
  return <p>never</p>;
}
