"use client";
import { useState } from "react";

export default function LayoutCounter() {
  const [n, setN] = useState(0);
  return <button id="layout-count" onClick={() => setN(n + 1)}>{n}</button>;
}
