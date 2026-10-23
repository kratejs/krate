import { useState } from "react";

// Array patterns that skip the getter (`[, setTick]`) or the setter
// (`[label]`) of useState still bind the other element to the right position.
export default function UseStatePatternHoles() {
  const [, setTick] = useState(0);
  const [label] = useState("static");
  const [n, setN] = useState(0);
  return (
    <button
      id="go"
      onClick={() => {
        setTick((t) => t + 1);
        setN(n + 1);
      }}
    >
      {label} {n}
    </button>
  );
}
