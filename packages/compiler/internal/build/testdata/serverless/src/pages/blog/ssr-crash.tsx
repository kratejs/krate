export const config = { ssr: true };
export default function Crash() {
  if (Math.random() >= 0) throw new Error("secret crash detail");
  return <p>never</p>;
}
