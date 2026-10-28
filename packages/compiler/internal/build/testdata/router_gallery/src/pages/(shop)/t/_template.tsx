import { useState } from 'react';
export default function T({ children }) {
  const [n, setN] = useState(0);
  return <div class="tpl"><button id="tpl-count" onClick={() => setN(n + 1)}>{n}</button>{children}</div>;
}
