import { createSignal, createResource } from "@krate/runtime";
import "./demo.css";

type Item = { id: string; name: string; group: string; summary: string; builtWith: string; import: string };
const IDS = ["tabs", "dialog", "command", "select", "slider", "toast"];

// Static JSON files stand in for an API. The fetcher waits a moment so the
// loading state is visible, and "Break the request" points it at a missing
// file to show the error state.
export default function Demo() {
  const [id, setId] = createSignal("tabs");
  const [broken, setBroken] = createSignal(false);

  const [item, { refetch }] = createResource(
    () => id(),
    async (name): Promise<Item> => {
      await new Promise((r) => setTimeout(r, 600));
      const file = broken() ? "missing" : name;
      const res = await fetch("/demos/data/components/" + file + ".json");
      if (!res.ok) throw new Error("HTTP " + res.status);
      return res.json();
    },
  );

  return (
    <div class="dt">
      <div class="dt-pick dk-row" role="group" aria-label="Component">
        {IDS.map((name) => (
          <button type="button" class="dk-btn" aria-pressed={id() === name} onClick={() => setId(name)}>{name}</button>
        ))}
      </div>

      <div class="dt-card" aria-live="polite" aria-busy={item.loading}>
        <p class="dt-state dk-mono">state: <strong>{item.state}</strong></p>
        {item.loading && <p class="dk-muted">Loading {id()}...</p>}
        {item.error && !item.loading && (
          <div role="alert" class="dt-error">
            <p>Could not load that component ({(item.error as Error).message}).</p>
            <button type="button" class="dk-btn" onClick={() => { setBroken(false); refetch(); }}>Try again</button>
          </div>
        )}
        {item() && !item.loading && !item.error && (
          <article>
            <h2>{item()!.name} <span class="dt-group">{item()!.group}</span></h2>
            <p>{item()!.summary}</p>
            <p class="dk-muted">Built with: {item()!.builtWith}</p>
            <code class="dt-code">{item()!.import}</code>
          </article>
        )}
      </div>

      <label class="dt-break">
        <input type="checkbox" checked={broken()} onChange={(e) => { setBroken((e.target as HTMLInputElement).checked); refetch(); }} />
        Break the request (404)
      </label>
    </div>
  );
}
