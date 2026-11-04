"use client";
import { useState } from "react";

function Bomb({ armed }: { armed: boolean }) {
  if (armed) throw new Error("boom from the crash page");
  return <p id="crash-ok">the crash page is fine</p>;
}

export default function CrashPage() {
  const [armed, setArmed] = useState(false);
  return (
    <main id="crash">
      <button id="arm" onClick={() => setArmed(true)}>Arm</button>
      <Bomb armed={armed} />
    </main>
  );
}
