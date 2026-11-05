import { usePathname, useSelectedLayoutSegment, useSelectedLayoutSegments } from "@krate/runtime";
import { useState } from "react";

export default function Root({ children }: any) {
  const pathname = usePathname();
  const seg = useSelectedLayoutSegment();
  const segs = useSelectedLayoutSegments();
  const [n, setN] = useState(0);
  return (
    <div>
      <nav id="nav">
        <Link id="to-home" href="/" class={pathname === "/" ? "active" : "idle"}>Home</Link>
        <Link id="to-dash" href="/dash/" class={pathname === "/dash" ? "active" : "idle"}>Dash</Link>
        <Link id="to-settings" href="/dash/settings/">Settings</Link>
        <Link id="to-a" href="/blog/a/">A</Link>
        <Link id="to-b" href="/blog/b/">B</Link>
        <Link id="to-zzz" href="/blog/zzz/">ZZZ</Link>
        <Link id="to-docs" href="/docs/x/y/z/">Docs</Link>
        <Link id="to-ssr" href="/ssr/42/?tab=info">SSR</Link>
        <button id="layout-count" onClick={() => setN(n + 1)}>{n}</button>
        <span id="nav-path">{pathname}</span>
        <span id="nav-seg">{String(seg)}</span>
        <span id="nav-segs">{segs.join("|")}</span>
      </nav>
      <div id="children">{children}</div>
    </div>
  );
}
