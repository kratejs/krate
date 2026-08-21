import './code.css';
import { createSignal, onMount } from '@krate/runtime';

export interface CodeProps {
  children?: any;
  lang?: string;
  title?: string;
  showCopy?: boolean;
}

export default function Code(props: CodeProps) {
  var children = props.children || "";
  var lang = props.lang || "";
  var title = props.title || "";
  var showCopy = props.showCopy !== false;
  var [copied, setCopied] = createSignal(false);
  var codeRef: HTMLElement | null = null;

  onMount(function () {
    if (!codeRef) return;
    var el = codeRef;
    if (typeof el.textContent === "string" && el.textContent.length > 0) {
      el.setAttribute("data-code", el.textContent);
    }
  });

  function handleCopy() {
    if (!codeRef) return;
    var text = codeRef.textContent || "";
    if (typeof navigator !== "undefined" && navigator.clipboard) {
      navigator.clipboard.writeText(text).then(function () {
        setCopied(true);
        setTimeout(function () { setCopied(false); }, 2000);
      });
    }
  }

  return (
    <div class="krate-code-block">
      {title !== "" || showCopy ? (
        <div class="krate-code-header">
          {title !== "" ? <div class="krate-code-title">{title}</div> : <div class="krate-code-title"></div>}
          {showCopy ? (
            <button class="krate-code-copy" onClick={handleCopy} type="button">
              {copied() ? (
                <span class="krate-code-copy-icon">&#10003;</span>
              ) : (
                <span class="krate-code-copy-icon">&#128203;</span>
              )}
            </button>
          ) : null}
        </div>
      ) : null}
      <pre><code ref={codeRef} class={lang !== "" ? "language-" + lang : ""}>{children}</code></pre>
    </div>
  );
}
