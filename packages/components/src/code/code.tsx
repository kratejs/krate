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
  var wrapRef: HTMLElement | null = null;

  onMount(function () {
    if (!wrapRef) return;
    var codeEl = wrapRef.querySelector("code");
    if (codeEl && typeof codeEl.textContent === "string" && codeEl.textContent.length > 0) {
      codeEl.setAttribute("data-code", codeEl.textContent);
    }
  });

  function handleCopy() {
    if (!wrapRef) return;
    var codeEl = wrapRef.querySelector("code");
    var text = codeEl ? codeEl.textContent || "" : "";
    if (typeof navigator !== "undefined" && navigator.clipboard) {
      navigator.clipboard.writeText(text).then(function () {
        setCopied(true);
        setTimeout(function () { setCopied(false); }, 2000);
      });
    }
  }

  return (
    <div class="krate-code-block" ref={wrapRef}>
      {title !== "" || showCopy ? (
        <div class="krate-code-header">
          {title !== "" ? <div class="krate-code-title">{title}</div> : <div class="krate-code-title"></div>}
          {showCopy ? (
            <button class="krate-code-copy" onClick={handleCopy} type="button" aria-label={copied() ? "Copied" : "Copy code"}>
              {copied() ? (
                <Icon name="tabler:check" width="14" height="14" />
              ) : (
                <Icon name="tabler:copy" width="14" height="14" />
              )}
            </button>
          ) : null}
        </div>
      ) : null}
      <SyntaxHighlight lang={lang}>{children}</SyntaxHighlight>
    </div>
  );
}
