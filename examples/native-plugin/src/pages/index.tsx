import { createSignal, createEffect } from '@krate/runtime';
// The generated module of the plugin: typed from its Go code, with no glue to
// write. Info, Memory and CPULoad are calls, Load is a host signal.
import { Info, Memory, CPULoad, Load } from 'krate/plugin/system-info';

export default function Home() {
  const [info, setInfo] = createSignal(null);
  const [memory, setMemory] = createSignal(null);
  const [load, setLoad] = createSignal(0);
  const [error, setError] = createSignal('');

  createEffect(() => {
    Info().then(setInfo, (e) => setError(e.code + ': ' + e.message));
    Memory().then(setMemory, (e) => setError(e.code + ': ' + e.message));
  });

  const watch = async () => {
    try {
      for await (const v of CPULoad(500)) setLoad(v);
    } catch (e) {
      setError(e.code + ': ' + e.message);
    }
  };

  return (
    <main>
      <h1>System info</h1>
      <p id="os">{info() ? info().os + '/' + info().arch + ' on ' + info().hostname : '...'}</p>
      <p id="cpus">{info() ? info().cpus + ' CPUs' : ''}</p>
      <p id="memory">{memory() ? Math.round(memory().total / 1048576) + ' MiB' : ''}</p>
      <p id="load">CPU load (stream): {load().toFixed(1)}%</p>
      <p id="sampled">CPU load (signal): {Load().toFixed(1)}%</p>
      <button id="watch" onClick={watch}>Watch CPU load</button>
      <p id="error">{error()}</p>
    </main>
  );
}
