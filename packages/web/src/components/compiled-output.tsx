import { createCSSChoice } from "@krate/runtime";
import { CompiledTsx, CompiledHtml, CompiledJs, CompiledFacts } from "../home/panes";

// What the compiler emitted for the counter above, as built by the playground
// engine (scripts/build-compiled-demo.mjs). The tabs are a CSS signal.
export function CompiledOutput() {
  const view = createCSSChoice("html", { options: ["tsx", "html", "js"], label: "Compiler output" });
  return (
    <section class="demo-source co" aria-label="Compiled output">
      <h2>What the compiler emitted</h2>
      <p class="co-lede">
        The component is lowered to static HTML with two comment markers and a <CompiledFacts kind="summary" />.
        The runtime is a shared chunk the browser caches across pages.
      </p>
      <div class="co-tabs">
        <button type="button" {...view.trigger("tsx")}>TSX in</button>
        <button type="button" {...view.trigger("html")}>HTML out (<CompiledFacts kind="html" />)</button>
        <button type="button" {...view.trigger("js")}>Page JS out (<CompiledFacts kind="js" />)</button>
      </div>
      <div {...view.panel("tsx")}><CompiledTsx /></div>
      <div {...view.panel("html")}><CompiledHtml /></div>
      <div {...view.panel("js")}><CompiledJs /></div>
    </section>
  );
}
