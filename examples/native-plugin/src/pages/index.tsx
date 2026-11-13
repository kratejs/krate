import { createSignal, createEffect } from '@krate/runtime';
// The generated module of the plugin: typed from its Go code, with no glue to
// write. info, memory and cpuLoad are calls, load is a host signal.
import { info, memory, cpuLoad, load } from 'krate/plugin/system-info';

export default function Home() {
  const [sys, setSys] = createSignal(null);
  const [mem, setMem] = createSignal(null);
  const [pct, setPct] = createSignal(0);
  const [error, setError] = createSignal('');

  createEffect(() => {
    info().then(setSys, (e) => setError(e.code + ': ' + e.message));
    memory().then(setMem, (e) => setError(e.code + ': ' + e.message));
  });

  const watch = async () => {
    try {
      for await (const v of cpuLoad(500)) setPct(v);
    } catch (e) {
      setError(e.code + ': ' + e.message);
    }
  };

  return (
    <main>
      <h1>System info</h1>
      <p id="os">{sys() ? sys().os + '/' + sys().arch + ' on ' + sys().hostname : '...'}</p>
      <p id="cpus">{sys() ? sys().cpus + ' CPUs' : ''}</p>
      <p id="memory">{mem() ? Math.round(mem().total / 1048576) + ' MiB' : ''}</p>
      <p id="load">CPU load (stream): {pct().toFixed(1)}%</p>
      {/* A host signal reads undefined until the host sends its first value. */}
      <p id="sampled">CPU load (signal): {(load() ?? 0).toFixed(1)}%</p>
      <button id="watch" onClick={watch}>Watch CPU load</button>
      <p id="error">{error()}</p>
    </main>
  );
}
