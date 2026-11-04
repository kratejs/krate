import { useState, type ComponentType } from "react";

function Button(props: { label: string; count?: number; onClick?: () => void }) {
  return <button id="hoc" onClick={props.onClick}>{props.label}:{props.count}</button>;
}

function withCounter<P extends object>(Wrapped: ComponentType<P & { count: number; onClick: () => void }>) {
  return function WithCounter(props: P) {
    const [count, setCount] = useState(0);
    return <Wrapped {...props} count={count} onClick={() => setCount(count + 1)} />;
  };
}

const CounterButton = withCounter(Button);

export default function Hoc() {
  return (
    <div>
      <CounterButton label="clicks" />
    </div>
  );
}
