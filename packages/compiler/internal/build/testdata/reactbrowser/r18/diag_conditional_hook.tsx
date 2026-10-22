import { useEffect, useState } from "react";

// Calling a hook conditionally breaks the Rules of Hooks; React's behavior is
// undefined (it throws when the hook count changes). Krate must reject it
// with a diagnostic instead of compiling something different.
export default function DiagConditionalHook(props: { tracking?: boolean }) {
  const [n, setN] = useState(0);
  if (props.tracking) {
    useEffect(() => {
      document.title = "count " + n;
    });
  }
  return <button onClick={() => setN(n + 1)}>{n}</button>;
}
