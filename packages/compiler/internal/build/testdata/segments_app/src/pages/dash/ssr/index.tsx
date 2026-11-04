export const config = { ssr: true };
export default function SsrThrow() {
  if (Math.random() >= 0) throw new Error('secret server failure');
  return <p>never</p>;
}
