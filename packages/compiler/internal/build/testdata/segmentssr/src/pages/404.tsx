import { headers } from "@krate/runtime/server";

export const config = { ssr: true };

export default async function NotFound() {
  const h = await headers();
  return (
    <main>
      <Head>
        <title>{"Missing " + h.get("x-site")}</title>
      </Head>
      <h1>missing for {h.get("x-site")}</h1>
    </main>
  );
}
