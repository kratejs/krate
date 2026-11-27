import { createSignal, onMount } from "@krate/runtime";
import { Checkbox } from "@krate/components/checkbox";
import "@krate/components/src/select/select.css";
import "../styles/playground.css";

// The playground shell is one small Krate client component. It owns the
// reactive chrome (engine progress, build stats, diagnostic counts); the
// editor, tokenizer and preview wiring are plain JS in /playground/ui/app.js,
// loaded on mount so the island stays tiny. app.js reports through
// "pg:state" events on window (and keeps the merged state in window.__pgState).

// The pickers are the Krate select look (krate-select classes and tokens, CSS
// from @krate/components/select) driven by plain JS in /playground/ui/app.js:
// they need to be refilled and set from outside, which the Select component
// does not allow. app.js fills the list and wires the keyboard behaviour.
function Picker(props: { id: string; labelledby: string; placeholder: string }) {
  return (
    <div class="krate-select pg-select" id={props.id} data-pg-select>
      <button class="krate-select-trigger" type="button" role="combobox" aria-haspopup="listbox" aria-expanded="false" aria-labelledby={props.labelledby}>
        <span class="krate-select-value">{props.placeholder}</span>
        <span class="krate-select-icon" aria-hidden="true">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="m6 9 6 6 6-6" />
          </svg>
        </span>
      </button>
    </div>
  );
}

const mb = (n: number) => (n / 1048576).toFixed(1);
const kb = (n: number) => (n < 1024 ? n + " B" : (n / 1024).toFixed(1) + " KB");

export function Playground() {
  const [stage, setStage] = createSignal("idle");
  const [loaded, setLoaded] = createSignal(0);
  const [total, setTotal] = createSignal(0);
  const [version, setVersion] = createSignal("");
  const [message, setMessage] = createSignal("");
  const [buildMs, setBuildMs] = createSignal(-1);
  const [jsGz, setJsGz] = createSignal(-1);
  const [errors, setErrors] = createSignal(0);
  const [warnings, setWarnings] = createSignal(0);
  const [logs, setLogs] = createSignal(0);
  const [note, setNote] = createSignal("");

  onMount(() => {
    const apply = (d: any) => {
      if (d.stage !== undefined) setStage(d.stage);
      if (d.loaded !== undefined) setLoaded(d.loaded);
      if (d.total !== undefined) setTotal(d.total);
      if (d.version !== undefined) setVersion(d.version);
      if (d.message !== undefined) setMessage(d.message);
      if (d.buildMs !== undefined) setBuildMs(d.buildMs);
      if (d.jsGz !== undefined) setJsGz(d.jsGz);
      if (d.errors !== undefined) setErrors(d.errors);
      if (d.warnings !== undefined) setWarnings(d.warnings);
      if (d.logs !== undefined) setLogs(d.logs);
      if (d.note !== undefined) setNote(d.note);
    };
    const w = window as any;
    if (w.__pgState) apply(w.__pgState);
    window.addEventListener("pg:state", (e: Event) => apply((e as CustomEvent).detail));
    // A script element, not import(): a dynamic import makes the build keep
    // the whole runtime for every page of the site.
    const script = document.createElement("script");
    script.type = "module";
    script.src = "/playground/ui/app.js";
    script.onerror = () => {
      setStage("error");
      setMessage("Could not load the playground code.");
    };
    document.head.appendChild(script);
  });

  const busy = () => stage() !== "ready" && stage() !== "unavailable" && stage() !== "error";

  return (
    <section id="pg" class="pg" aria-label="Krate playground" data-stage={stage()}>
      <div class="pg-toolbar">
        <h1 class="pg-title">Playground</h1>
        <div class="pg-field">
          <span class="pg-label" id="pg-example-label">Example</span>
          <Picker id="pg-example" labelledby="pg-example-label" placeholder="Loading examples..." />
        </div>
        <div class="pg-actions">
          <button id="pg-run" type="button" class="pg-btn pg-btn-primary" title="Rebuild now (Ctrl+Enter)">Run</button>
          <button id="pg-share" type="button" class="pg-btn" title="Copy a link to this project (Ctrl+S)">Share</button>
          <button id="pg-reset" type="button" class="pg-btn" title="Restore the selected example">Reset</button>
        </div>
        <div class="pg-opts">
          <Checkbox id="pg-minify" class="pg-check">Minify</Checkbox>
          <Checkbox id="pg-dev" class="pg-check">Dev mode</Checkbox>
        </div>
        <span class="pg-engine" role="status" aria-live="polite">
          {stage() === "ready" ? "Krate compiler " + version() : ""}
          {busy() && stage() !== "idle" ? "Loading compiler..." : ""}
        </span>
      </div>

      <div class="pg-main" id="pg-main">
        <div class="pg-left" id="pg-left">
          <div class="pg-tabbar">
            <div role="tablist" aria-label="Project files" id="pg-files" class="pg-tabs"></div>
            <form id="pg-name-form" class="pg-name-form" hidden>
              <label class="sr-only" for="pg-name">File name</label>
              <input id="pg-name" class="pg-name" type="text" spellcheck={false} autocomplete="off" placeholder="components/card.tsx" />
              <button type="submit" class="pg-btn pg-btn-primary">OK</button>
              <button type="button" id="pg-name-cancel" class="pg-btn">Cancel</button>
            </form>
            <div class="pg-tabtools" id="pg-tabtools">
              <button id="pg-add" type="button" class="pg-icon-btn" aria-label="Add a file" title="Add a file">+</button>
              <button id="pg-rename" type="button" class="pg-icon-btn" aria-label="Rename this file" title="Rename this file (F2)">Aa</button>
              <button id="pg-delete" type="button" class="pg-icon-btn" aria-label="Delete this file" title="Delete this file">&#215;</button>
            </div>
          </div>
          <div id="pg-editor" class="pg-editor" role="tabpanel" aria-label="Editor"></div>
        </div>

        <div id="pg-split" class="pg-split" role="separator" aria-orientation="vertical" aria-label="Resize the editor and output panes" aria-valuemin={20} aria-valuemax={80} aria-valuenow={50} tabindex={0}></div>

        <div class="pg-right" id="pg-right">
          <div class="pg-tabbar">
            <div role="tablist" aria-label="Output" id="pg-out-tabs" class="pg-tabs">
              <button type="button" role="tab" id="pg-tab-preview" aria-controls="pg-panel-preview" aria-selected="true" data-tab="preview">Preview</button>
              <button type="button" role="tab" id="pg-tab-html" aria-controls="pg-panel-html" aria-selected="false" tabindex={-1} data-tab="html">HTML</button>
              <button type="button" role="tab" id="pg-tab-js" aria-controls="pg-panel-js" aria-selected="false" tabindex={-1} data-tab="js">JS</button>
              <button type="button" role="tab" id="pg-tab-css" aria-controls="pg-panel-css" aria-selected="false" tabindex={-1} data-tab="css">CSS</button>
              <button type="button" role="tab" id="pg-tab-files" aria-controls="pg-panel-files" aria-selected="false" tabindex={-1} data-tab="files">Files</button>
              <button type="button" role="tab" id="pg-tab-diagnostics" aria-controls="pg-panel-diagnostics" aria-selected="false" tabindex={-1} data-tab="diagnostics">
                Diagnostics <span class={"pg-badge" + (errors() > 0 ? " pg-badge-error" : "")}>{errors() + warnings()}</span>
              </button>
              <button type="button" role="tab" id="pg-tab-console" aria-controls="pg-panel-console" aria-selected="false" tabindex={-1} data-tab="console">
                Console <span class="pg-badge">{logs()}</span>
              </button>
            </div>
          </div>

          <div class="pg-panels">
            <div role="tabpanel" id="pg-panel-preview" aria-labelledby="pg-tab-preview" class="pg-panel pg-panel-preview">
              <div class="pg-urlbar">
                <button id="pg-reload" type="button" class="pg-icon-btn" aria-label="Reload the preview" title="Reload the preview">&#8635;</button>
                <output id="pg-url" class="pg-url" aria-label="Preview path">/</output>
                <a id="pg-popout" class="pg-icon-btn" href="/playground/" target="_blank" rel="noopener" aria-label="Open the preview in a new tab" title="Open in a new tab">&#8599;</a>
              </div>
              <div class="pg-frames" id="pg-frames"></div>
              <div class="pg-gate" id="pg-gate" hidden>
                <p><strong>This project came from a shared link.</strong> Its code runs on this site only if you choose to.</p>
                <button id="pg-gate-run" type="button" class="pg-btn pg-btn-primary">Run the preview</button>
              </div>
              <div class="pg-boot" id="pg-boot" hidden={!busy() && stage() !== "unavailable" && stage() !== "error"}>
                {stage() === "unavailable" ? (
                  <div class="pg-card">
                    <h2>The compiler is not available</h2>
                    <p>
                      This copy of the site was built without the WebAssembly compiler, so the playground cannot run here.
                      Install Krate and run it locally instead, or visit krate.js.org.
                    </p>
                    <p><a class="pg-link" href="/docs/getting-started/">Getting started with Krate</a></p>
                  </div>
                ) : stage() === "error" ? (
                  <div class="pg-card">
                    <h2>The compiler stopped</h2>
                    <p>{message()}</p>
                    <button id="pg-retry" type="button" class="pg-btn">Try again</button>
                  </div>
                ) : (
                  <div class="pg-card">
                    <h2>Loading the Krate compiler</h2>
                    <p>
                      The real compiler runs in your browser as WebAssembly.
                      {stage() === "download" ? " Downloaded " + mb(loaded()) + (total() > 0 ? " of " + mb(total()) : "") + " MB." : ""}
                      {stage() === "compile" ? " Compiling..." : ""}
                      {stage() === "start" ? " Starting..." : ""}
                    </p>
                    <progress class="pg-progress" max={total() > 0 ? total() : 1} value={total() > 0 ? loaded() : 0} aria-label="Compiler download progress"></progress>
                  </div>
                )}
              </div>
            </div>

            <div role="tabpanel" id="pg-panel-html" aria-labelledby="pg-tab-html" class="pg-panel" hidden>
              <div class="pg-codehead"><span class="pg-label" id="pg-sel-html-label">File</span><Picker id="pg-sel-html" labelledby="pg-sel-html-label" placeholder="No files" /></div>
              <pre class="pg-code" id="pg-code-html" tabindex={0} aria-label="Generated HTML"></pre>
            </div>
            <div role="tabpanel" id="pg-panel-js" aria-labelledby="pg-tab-js" class="pg-panel" hidden>
              <div class="pg-codehead"><span class="pg-label" id="pg-sel-js-label">File</span><Picker id="pg-sel-js" labelledby="pg-sel-js-label" placeholder="No files" /></div>
              <pre class="pg-code" id="pg-code-js" tabindex={0} aria-label="Generated JavaScript"></pre>
            </div>
            <div role="tabpanel" id="pg-panel-css" aria-labelledby="pg-tab-css" class="pg-panel" hidden>
              <div class="pg-codehead"><span class="pg-label" id="pg-sel-css-label">File</span><Picker id="pg-sel-css" labelledby="pg-sel-css-label" placeholder="No files" /></div>
              <pre class="pg-code" id="pg-code-css" tabindex={0} aria-label="Generated CSS"></pre>
            </div>
            <div role="tabpanel" id="pg-panel-files" aria-labelledby="pg-tab-files" class="pg-panel" hidden>
              <div class="pg-scroll" id="pg-files-out"></div>
            </div>
            <div role="tabpanel" id="pg-panel-diagnostics" aria-labelledby="pg-tab-diagnostics" class="pg-panel" hidden>
              <div class="pg-scroll" id="pg-diags"></div>
            </div>
            <div role="tabpanel" id="pg-panel-console" aria-labelledby="pg-tab-console" class="pg-panel" hidden>
              <div class="pg-codehead"><button id="pg-clear" type="button" class="pg-btn">Clear</button></div>
              <div class="pg-scroll pg-console" id="pg-console" role="log" aria-label="Preview console"></div>
            </div>
          </div>
        </div>
      </div>

      <div class="pg-statusbar" role="status">
        <span class="pg-stat">{buildMs() >= 0 ? "Built in " + Math.round(buildMs()) + " ms" : stage() === "ready" ? "Ready" : "Waiting for the compiler"}</span>
        <span class="pg-stat">{jsGz() >= 0 ? "JS " + kb(jsGz()) + " gzip" : ""}</span>
        <span class={"pg-stat" + (errors() > 0 ? " pg-stat-error" : "")}>
          {errors() > 0 ? errors() + (errors() === 1 ? " error" : " errors") : buildMs() >= 0 ? "No errors" : ""}
        </span>
        <span class="pg-stat pg-stat-note" aria-live="polite">{note()}</span>
      </div>
      <noscript>
        <p class="pg-noscript">The playground needs JavaScript to run the compiler in your browser.</p>
      </noscript>
    </section>
  );
}
