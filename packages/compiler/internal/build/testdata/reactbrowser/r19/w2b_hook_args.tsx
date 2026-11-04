import { useEffect, useState } from "react";
import { log } from "../lib/log";

function useDebounce<T>(value: T, ms: number): T {
  const [d, setD] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setD(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return d;
}

function useDocumentTitle(title: string) {
  useEffect(() => {
    document.title = title;
    log("title", title);
  }, [title]);
}

export default function HookArgs() {
  const [n, setN] = useState(0);
  const d = useDebounce(n, 20);
  useDocumentTitle("count " + n);
  return (
    <div>
      <button id="inc" onClick={() => setN(n + 1)}>inc</button>
      <p id="now">{n}</p>
      <p id="deb">{d}</p>
    </div>
  );
}
