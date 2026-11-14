import { env } from "cloudflare:workers";

export const config = { ssr: true };

function readEnv() {
  const e = env as any;
  return e;
}

export default function NotFoundPage() {
  const e = readEnv();
  return <main><h1 id="page">custom-404</h1><p>{String(e.DB)}</p><a href={"/" + e.DB} title={e.KV}>Home</a></main>;
}
