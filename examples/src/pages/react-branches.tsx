import { createSignal, onCleanup, onMount } from '@krate/runtime';
import { useEffect, useRef, useState } from 'react';

// One component type in both branches of a conditional keeps its instance:
// state, typed input, refs and effects survive the flip, and only the props
// change. Different keys still remount.

function Form(props: { mode: string, hint?: string, onLog: (line: string) => void }) {
  const [text, setText] = useState('');
  const [clicks, setClicks] = useState(0);
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => {
    props.onLog('mount ' + props.mode + (input.current ? ' ref' : ' noref'));
    return () => props.onLog('unmount ' + props.mode);
  }, []);
  return (
    <form data-testid="form" class="flex gap-2">
      <b data-testid="mode">{props.mode}</b>
      <input data-testid="input" ref={input} value={text} onChange={(e: any) => setText(e.target.value)} />
      <button type="button" data-testid="clicks" onClick={() => setClicks(clicks + 1)}>{clicks}</button>
      <em data-testid="hint">{props.hint ?? 'none'}</em>
    </form>
  );
}

function Native(props: { label: string }) {
  const [n, setN] = createSignal(0);
  onMount(() => {
    (window as any).__branchLog = [...((window as any).__branchLog || []), 'native mount ' + props.label];
  });
  onCleanup(() => {
    (window as any).__branchLog = [...((window as any).__branchLog || []), 'native cleanup'];
  });
  return <button data-testid="native" onClick={() => setN(n() + 1)}>{props.label}:{n()}</button>;
}

function Early(props: { alt: boolean }) {
  const [n, setN] = useState(0);
  return <button data-testid="early" onClick={() => setN(n + 1)}>{props.alt ? 'alt' : 'main'}:{n}</button>;
}

function EarlyReturn(props: { alt: boolean }) {
  if (props.alt) return <Early alt />;
  return <Early alt={false} />;
}

export default function ReactBranches() {
  const [isEdit, setEdit] = useState(false);
  const [log, setLog] = useState<string[]>([]);
  const onLog = (line: string) => setLog((l) => [...l, line]);
  return (
    <main>
      <button data-testid="flip" onClick={() => setEdit(!isEdit)}>flip</button>
      <section data-testid="ternary">
        {isEdit ? <Form mode="edit" hint="h" onLog={onLog} /> : <Form mode="new" onLog={onLog} />}
      </section>
      <section data-testid="native-section">
        {isEdit ? <Native label="native-edit" /> : <Native label="native-new" />}
      </section>
      <section data-testid="early-section">
        <EarlyReturn alt={isEdit} />
      </section>
      <section data-testid="keyed">
        {isEdit ? <Early key="a" alt /> : <Early key="b" alt={false} />}
      </section>
      <ul data-testid="log">{log.map((line, i) => <li key={i}>{line}</li>)}</ul>
    </main>
  );
}
