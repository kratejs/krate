import { createSignal } from '@krate/runtime';
import { onOpenFile } from '@krate/app/app';
import { onFileDrop } from '@krate/app/window';
import { readText } from '@krate/app/fs';

// Files arrive three ways: on the command line of the first launch, from a
// second launch (a double click while the app runs) and dropped on the window.
// The app may read each one through the fs API: a file the user opens or drops
// is granted like one chosen in a dialog.
export default function Home() {
  const [opened, setOpened] = createSignal<string[]>([]);
  const [dropped, setDropped] = createSignal<string[]>([]);
  const [problems, setProblems] = createSignal<string[]>([]);

  async function show(path: string): Promise<string> {
    return path + ' => ' + (await readText(path)).trim();
  }

  onOpenFile((path) => {
    show(path).then(
      (line) => setOpened([...opened(), line]),
      (e) => setProblems([...problems(), 'open ' + path + ': ' + String(e)]),
    );
  });
  onFileDrop((e) => {
    for (const path of e.paths) {
      show(path).then(
        (line) => setDropped([...dropped(), line]),
        (err) => setProblems([...problems(), 'drop ' + path + ': ' + String(err)]),
      );
    }
  });

  return (
    <main>
      <h1>Native Files</h1>
      <p>Drop a file here, or open a .krnote file with this app.</p>
      <ul id="opened">{opened().map((l) => <li key={l}>{l}</li>)}</ul>
      <ul id="dropped">{dropped().map((l) => <li key={l}>{l}</li>)}</ul>
      <p id="problems">{problems().join('; ')}</p>
    </main>
  );
}
