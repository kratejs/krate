import { createSignal, createResource } from "@krate/runtime";

// createResource refetches when its source changes and reports loading and
// error state. The JSON is a static file in this site.
export function FetchDemo() {
  const [id, setId] = createSignal("tabs");
  const [info] = createResource(
    () => id(),
    async (name) => (await fetch("/demos/data/components/" + name + ".json")).json(),
  );
  return (
    <div class="fd">
      <div class="fd-pick">
        <button type="button" aria-pressed={id() === "tabs"} onClick={() => setId("tabs")}>tabs</button>
        <button type="button" aria-pressed={id() === "dialog"} onClick={() => setId("dialog")}>dialog</button>
        <button type="button" aria-pressed={id() === "slider"} onClick={() => setId("slider")}>slider</button>
      </div>
      <p class="fd-out" aria-live="polite">{info.loading ? "Loading..." : info() ? info().summary : "Pick one."}</p>
    </div>
  );
}
