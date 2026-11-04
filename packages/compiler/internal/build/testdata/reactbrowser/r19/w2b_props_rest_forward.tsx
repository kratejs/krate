import { useState, type ButtonHTMLAttributes } from "react";
import { log } from "../lib/log";

function Button({ variant = "solid", className, children, ...props }: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: string }) {
  return (
    <button className={"btn " + variant + (className ? " " + className : "")} {...props}>
      {children}
    </button>
  );
}

function Tag(props: { label: string; tone?: string; title?: string }) {
  const { label, ...rest } = props;
  return <span id="tag" {...rest}>[{label}]</span>;
}

export default function PropsRestForward() {
  const [n, setN] = useState(0);
  return (
    <div>
      <Button id="b1" variant={n % 2 ? "ghost" : "solid"} className="extra" disabled={n >= 3} onClick={() => { setN(n + 1); log("click", n + 1); }}>
        go {n}
      </Button>
      <Tag label={"n" + n} title={"t" + n} tone="x" />
    </div>
  );
}
