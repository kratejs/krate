import { useState } from 'react';
export default function InnerLayout({ children }) {
  const [n, setN] = useState(0);
  return (
    <section id="inner-layout">
      <button id="inner-count" onClick={() => setN(n + 1)}>{n}</button>
      {children}
    </section>
  );
}
