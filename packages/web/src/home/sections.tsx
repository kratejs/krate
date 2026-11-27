import { CounterDemo } from "../components/counter-demo";
import { createCSSChoice } from "@krate/runtime";
import { FetchDemo } from "./fetch-demo";
import { ThemeSwitch } from "./toggle-demo";
import { Snippet, CompiledTsx, CompiledHtml, CompiledJs, CompiledFacts, BenchTable, BenchNote } from "./panes";

// Source on the left, live preview on the right; the example is a CSS choice,
// so switching tabs needs no JavaScript. Only the counter and the fetch
// preview are islands.
export function HeroTabs() {
  const ex = createCSSChoice("counter", { options: ["counter", "css", "fetch"], label: "Example" });
  return (
    <div class="ht">
      <div class="ht-tabs" role="group" aria-label="Examples">
        <button type="button" {...ex.trigger("counter")}>Signals</button>
        <button type="button" {...ex.trigger("css")}>CSS signals</button>
        <button type="button" {...ex.trigger("fetch")}>Resources</button>
      </div>

      <div {...ex.panel("counter")} class="ht-panel">
        <div class="ht-code"><Snippet name="counter.tsx" /></div>
        <div class="ht-live">
          <span class="ht-live-label">Live preview</span>
          <CounterDemo />
          <span class="ht-live-note">Signals update the DOM directly - no virtual DOM.</span>
          <a class="ht-open" href="/playground/#example=counter">Open in the playground</a>
        </div>
      </div>

      <div {...ex.panel("css")} class="ht-panel">
        <div class="ht-code"><Snippet name="toggle-demo.tsx" /></div>
        <div class="ht-live">
          <span class="ht-live-label">Live preview</span>
          <ThemeSwitch />
          <span class="ht-live-note">Zero JavaScript. Turn it off in your browser and this still works.</span>
          <a class="ht-open" href="/playground/#example=demo-css-signals">Open in the playground</a>
        </div>
      </div>

      <div {...ex.panel("fetch")} class="ht-panel">
        <div class="ht-code"><Snippet name="fetch-demo.tsx" /></div>
        <div class="ht-live">
          <span class="ht-live-label">Live preview</span>
          <FetchDemo />
          <span class="ht-live-note">createResource tracks loading and errors for you.</span>
          <a class="ht-open" href="/playground/#example=demo-data">Open in the playground</a>
        </div>
      </div>
    </div>
  );
}

// TSX in, HTML and a small script out: the four stages, with the real output
// of the counter from the compiled-output demo.
export function Pipeline() {
  const step = createCSSChoice("tsx", { options: ["tsx", "parse", "html", "js"], label: "Compiler stage" });
  return (
    <div class="pl">
      <ol class="pl-steps">
        <li><button type="button" {...step.trigger("tsx")}>1 - Your TSX</button></li>
        <li><button type="button" {...step.trigger("parse")}>2 - Parse and analyze</button></li>
        <li><button type="button" {...step.trigger("html")}>3 - Static HTML</button></li>
        <li><button type="button" {...step.trigger("js")}>4 - Tiny script</button></li>
      </ol>
      <div class="pl-body">
        <div {...step.panel("tsx")} class="pl-pane">
          <p>You write components with signals. A Go lexer and parser read the TSX; there is no Node and no bundler in the loop.</p>
          <CompiledTsx />
        </div>
        <div {...step.panel("parse")} class="pl-pane">
          <p>The compiler lowers the JSX to an intermediate tree and finds which values are reactive. Here it finds exactly two things to wire up:</p>
          <ul class="pl-found">
            <li><code>{"{count()}"}</code> is a text node bound to the <code>count</code> signal.</li>
            <li><code>onClick</code> is a handler on the button.</li>
            <li>Everything else, the paragraph, the classes, the labels, is static and costs nothing at runtime.</li>
          </ul>
        </div>
        <div {...step.panel("html")} class="pl-pane">
          <p>The page is rendered at build time. The reactive text carries two comment markers so the script can find it.</p>
          <CompiledHtml />
        </div>
        <div {...step.panel("js")} class="pl-pane">
          <p>Hydration binds those two spots: a <CompiledFacts kind="summary" />.</p>
          <CompiledJs />
        </div>
      </div>
      <p class="pl-more"><a href="/demos/compiled/">See the full compiled output</a></p>
    </div>
  );
}

// Each metric is a panel of a CSS choice and every panel is a real table, so
// the bars never hide data from assistive tech.
export function BenchChart() {
  const metric = createCSSChoice("build", { options: ["build", "js", "api"], label: "Metric" });
  return (
    <div class="bc">
      <div class="bc-tabs" role="group" aria-label="Benchmark metric">
        <button type="button" {...metric.trigger("build")}>Cold build time</button>
        <button type="button" {...metric.trigger("js")}>Client JS (gzip)</button>
        <button type="button" {...metric.trigger("api")}>API requests/s</button>
      </div>
      <div {...metric.panel("build")}><BenchTable metric="build" /></div>
      <div {...metric.panel("js")}><BenchTable metric="js" /></div>
      <div {...metric.panel("api")}><BenchTable metric="api" /></div>
      <BenchNote />
    </div>
  );
}
