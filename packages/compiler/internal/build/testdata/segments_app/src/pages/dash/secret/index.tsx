import { forbidden } from '@krate/runtime/server';
export const config = { ssr: true };
export default function Secret() {
  forbidden();
  return <p>never</p>;
}
