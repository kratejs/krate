import { createSignal, createEffect, onMount } from '@krate/runtime';

export default function EffectPage() {
  const [count, setCount] = createSignal(0);
  const [log, setLog] = createSignal('none');

  const doMount = () => {
    setLog('mounted');
  };

  onMount(doMount);

  if (count() < 10) {
    createEffect(() => {
      setLog('effect:' + count());
    });
  }

  return (
    <div class="page">
      <h1>Effect Demo</h1>
      <p>Count: <span>{count()}</span></p>
      <p>Log: <span>{log()}</span></p>
      <button onClick={() => setCount(count() + 1)}>+</button>
    </div>
  )
}