import { createContext, useContext, useState } from "react";

const Density = createContext<"compact" | "comfortable">("comfortable");

function Row(props: { label: string }) {
  const density = useContext(Density);
  return <li className={density}>{props.label}</li>;
}

// React 19: <Context value={...}> works as a provider without .Provider.
export default function ContextAsProvider() {
  const [density, setDensity] = useState<"compact" | "comfortable">("compact");
  return (
    <Density value={density}>
      <ul id="rows">
        <Row label="one" />
        <Row label="two" />
      </ul>
      <button id="toggle" onClick={() => setDensity(density === "compact" ? "comfortable" : "compact")}>
        toggle
      </button>
    </Density>
  );
}
