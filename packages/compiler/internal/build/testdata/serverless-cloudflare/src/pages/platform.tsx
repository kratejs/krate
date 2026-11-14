import { env } from "cloudflare:workers";
import { after, getCloudflareContext, getPlatform } from "@krate/runtime/server";

export const config = { ssr: true };

export default function Platform() {
  const cf = getCloudflareContext();
  after(() => {
    (globalThis as any).__afterRan = ((globalThis as any).__afterRan || 0) + 1;
  });
  return (
    <main>
      <p id="module-env">{String((env as any).DB)}</p>
      <p id="ctx-env">{String(cf.env.DB)}</p>
      <p id="country">{String(cf.cf && cf.cf.country)}</p>
      <p id="target">{getPlatform().target}</p>
    </main>
  );
}
