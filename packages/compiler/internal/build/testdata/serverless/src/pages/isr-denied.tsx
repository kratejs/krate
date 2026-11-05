import { forbidden } from "@krate/runtime/server";
export const revalidate = 60;
export default function IsrDenied() {
  forbidden();
  return <p>never</p>;
}
