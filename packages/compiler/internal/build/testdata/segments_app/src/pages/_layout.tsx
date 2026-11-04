import { useState } from 'react';
export default function Root({ children }) {
  const [n, setN] = useState(0);
  return (
    <div class="app">
      <nav id="nav">
        <Link id="to-home" href="/">Home</Link>
        <Link id="to-dash" href="/dash/">Dash</Link>
        <Link id="to-dash-missing" href="/dash/missing" prefetch={false}>Missing</Link>
        <Link id="to-root-missing" href="/nowhere" prefetch={false}>Nowhere</Link>
        <Link id="to-crash" href="/dash/crash/">Crash</Link>
        <Link id="to-broken" href="/dash/broken/">Broken</Link>
        <Link id="to-ssr" href="/dash/ssr/" prefetch={false}>SSR</Link>
        <button id="layout-count" onClick={() => setN(n + 1)}>{n}</button>
      </nav>
      <div id="children">{children}</div>
    </div>
  );
}
