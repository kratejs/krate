'use client';

import { useState } from 'react';

export default function Page() {
  const [count, setCount] = useState(0);
  const items = Array.from({ length: 100 }, (_, i) => i);

  return (
    <main>
      <h1>Benchmark</h1>
      <p>Count: {count}</p>
      <button onClick={() => setCount((c) => c + 1)}>+</button>
      <ul>
        {items.map((i) => (
          <li key={i}>Item {i}</li>
        ))}
      </ul>
    </main>
  );
}
