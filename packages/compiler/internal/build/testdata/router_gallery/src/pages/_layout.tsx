import { useState } from 'react';
export default function Root({ children, modal }) {
  const [n, setN] = useState(0);
  return (
    <div class="app">
      <nav>
        <Link href="/">Feed</Link>
        <Link href="/t/a">TA</Link>
        <button id="layout-count" onClick={() => setN(n + 1)}>{n}</button>
      </nav>
      <main id="children">{children}</main>
      <div id="modal-slot">{modal}</div>
    </div>
  );
}
