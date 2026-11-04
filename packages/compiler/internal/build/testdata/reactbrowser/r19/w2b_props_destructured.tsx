import { useState } from "react";

function Label({ value, tone = "x" }: { value: number; tone?: string }) {
  return <p id="label" className={tone}>value {value}</p>;
}

function Panel({ loading, items }: { loading: boolean; items: string[] }) {
  if (loading) return <p id="panel">loading</p>;
  return <p id="panel">{items.join(",")}</p>;
}

function Renamed({ value: v, ...rest }: { value: number; extra?: string }) {
  return <p id="renamed">{v}-{rest.extra}</p>;
}

export default function PropsDestructured() {
  const [v, setV] = useState(0);
  const [loading, setLoading] = useState(true);
  return (
    <div>
      <button id="inc" onClick={() => setV(v + 1)}>inc</button>
      <button id="load" onClick={() => setLoading(false)}>load</button>
      <Label value={v} tone={v % 2 ? "odd" : "even"} />
      <Panel loading={loading} items={["a", "b"]} />
      <Renamed value={v} extra={"e" + v} />
      {v >= 0 ? <Label value={v} /> : null}
    </div>
  );
}
