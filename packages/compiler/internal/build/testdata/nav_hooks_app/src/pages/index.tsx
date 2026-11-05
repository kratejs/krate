import { useRouter, useSearchParams, usePathname } from "@krate/runtime";

export default function Home() {
  const router = useRouter();
  const search = useSearchParams();
  const pathname = usePathname();
  return (
    <main>
      <h1 id="home">home {pathname}</h1>
      <p id="q">{String(search.get("q"))}</p>
      <button id="push" onClick={() => router.push("/blog/a/?q=pushed")}>push</button>
      <button id="replace" onClick={() => router.replace("/blog/b/")}>replace</button>
      <button id="back" onClick={() => router.back()}>back</button>
      <button id="forward" onClick={() => router.forward()}>forward</button>
      <button id="refresh" onClick={() => router.refresh()}>refresh</button>
      <button id="prefetch" onClick={() => router.prefetch("/dash/")}>prefetch</button>
    </main>
  );
}
