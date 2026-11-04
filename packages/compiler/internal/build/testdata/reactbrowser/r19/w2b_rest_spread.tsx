import { useState } from "react";
import { log } from "../lib/log";

function Btn(props: { label: string; className?: string; disabled?: boolean; onClick?: () => void }) {
  const { label, ...rest } = props;
  return <button id="btn" {...rest}>{label}</button>;
}

function useField(name: string) {
  const [v, setV] = useState("");
  return { value: v, name, onChange: (e: any) => setV(e.target.value) };
}

export default function RestSpread() {
  const [n, setN] = useState(0);
  const field = useField("email");
  return (
    <div>
      <Btn label={"go " + n} className="primary" onClick={() => { setN(n + 1); log("click", n + 1); }} disabled={n >= 2} />
      <p id="cond" {...(n > 0 ? { title: "x" } : {})}>cond</p>
      <input id="email" {...field} />
      <span id="echo">{field.value}</span>
    </div>
  );
}
