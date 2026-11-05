import { usePathname, useParams, useSearchParams } from "@krate/runtime";
export const config = { ssr: true };

export default function Ssr() {
  const pathname = usePathname();
  const params = useParams();
  const search = useSearchParams();
  return (
    <main>
      <p id="ssr-path">{pathname}</p>
      <p id="ssr-id">{params.id}</p>
      <p id="ssr-tab">{String(search.get("tab"))}</p>
    </main>
  );
}
