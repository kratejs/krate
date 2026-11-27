// Krate playground controller. Loaded by the Playground island on mount.
//
// Wires the static shell (src/components/playground.tsx) to the compiler
// engine worker (engine.js), the editor, the output panels and the preview
// service worker (preview/sw.js). Reactive chrome lives in the island and is
// fed through "pg:state" events.

import { createEditor } from "./editor.js";
import { highlightLines, langOf, pretty } from "./highlight.js";

const $ = (id) => document.getElementById(id);
const STORAGE_KEY = "krate-playground:v1";
const SPLIT_KEY = "krate-playground:split";
const MAX_FILES = 40;
const MAX_TOTAL = 400 * 1024;

// ---- Select pickers ----------------------------------------------------------
// The Krate select look (krate-select classes, styled by @krate/components)
// driven from here, because the lists are refilled and set at runtime. The
// keyboard model follows the Select component: ArrowDown/ArrowUp on the
// button open the list and focus the search field; there arrows, Home and End
// move the active option, Enter chooses, Escape closes and returns to the
// button, Tab closes, and typing filters.
const CHECK_SVG =
  '<span class="krate-select-item-indicator" aria-hidden="true"><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m5 12 5 5L20 7"/></svg></span>';

// Adds setOptions(list) and a value property to the picker root, and fires a
// bubbling "change" event when the user chooses. An option may be
// { value, label, hidden }: hidden ones are never listed but still name the
// current value.
function initPicker(root) {
  const trigger = root.querySelector(".krate-select-trigger");
  const valueEl = root.querySelector(".krate-select-value");
  const placeholder = valueEl.textContent;
  const uid = root.id + "-list";
  const optLabel = (o) => (o.label === undefined ? o.value : o.label);
  let options = [];
  let value = "";
  let query = "";
  let active = -1;
  let content = null;
  let filter = null;
  let viewport = null;

  const isOpen = () => content !== null;
  const shown = () => {
    const q = query.toLowerCase();
    return options.filter((o) => !o.hidden && (!q || optLabel(o).toLowerCase().includes(q) || o.value.toLowerCase().includes(q)));
  };
  const items = () => (viewport ? [...viewport.querySelectorAll(".krate-select-item")] : []);

  function renderValue() {
    const o = options.find((x) => x.value === value);
    valueEl.textContent = o ? optLabel(o) : placeholder;
  }

  function renderList() {
    const list = shown();
    viewport.innerHTML = list.length
      ? list
          .map(
            (o, i) =>
              `<div class="krate-select-item${o.value === value ? " krate-select-item-selected" : ""}" role="option" id="${uid}-opt-${i}" data-index="${i}" aria-selected="${o.value === value}"><span class="krate-select-item-text">${escHTML(optLabel(o))}</span>${o.value === value ? CHECK_SVG : ""}</div>`,
          )
          .join("")
      : '<div class="krate-select-empty">No results.</div>';
    setActive(list.length ? active : -1);
  }

  function setActive(i, scroll) {
    const els = items();
    active = els.length ? Math.max(0, Math.min(i, els.length - 1)) : -1;
    els.forEach((el, n) => {
      const on = n === active;
      el.classList.toggle("krate-select-item-active", on);
      if (on) {
        el.dataset.active = "true";
        if (scroll) el.scrollIntoView({ block: "nearest" });
      } else delete el.dataset.active;
    });
    if (active >= 0) filter.setAttribute("aria-activedescendant", `${uid}-opt-${active}`);
    else filter.removeAttribute("aria-activedescendant");
  }

  function openList() {
    if (isOpen()) return;
    query = "";
    const named = document.getElementById(trigger.getAttribute("aria-labelledby") || "");
    content = document.createElement("div");
    content.className = "krate-select-content";
    content.innerHTML =
      `<div class="krate-select-filter-wrapper"><input class="krate-select-filter" type="text" role="combobox" aria-expanded="true" aria-controls="${uid}" aria-autocomplete="list" placeholder="Search..." aria-label="Search options" autocomplete="off" spellcheck="false"></div>` +
      `<div class="krate-select-viewport" role="listbox" id="${uid}" aria-label="${escHTML(named ? named.textContent : "Options")}"></div>`;
    root.appendChild(content);
    filter = content.querySelector(".krate-select-filter");
    viewport = content.querySelector(".krate-select-viewport");
    trigger.setAttribute("aria-expanded", "true");
    trigger.setAttribute("aria-controls", uid);
    active = Math.max(0, shown().findIndex((o) => o.value === value));
    renderList();
    setActive(active, true);
    filter.focus();
    filter.addEventListener("input", () => {
      query = filter.value;
      active = 0;
      renderList();
    });
    filter.addEventListener("keydown", onFilterKey);
    viewport.addEventListener("mousedown", (e) => e.preventDefault());
    viewport.addEventListener("mousemove", (e) => {
      const el = e.target.closest(".krate-select-item");
      if (el && Number(el.dataset.index) !== active) setActive(Number(el.dataset.index));
    });
    viewport.addEventListener("click", (e) => {
      const el = e.target.closest(".krate-select-item");
      if (el) choose(shown()[Number(el.dataset.index)]);
    });
    document.addEventListener("mousedown", onDocDown, true);
  }

  function closeList(refocus) {
    if (!isOpen()) return;
    document.removeEventListener("mousedown", onDocDown, true);
    content.remove();
    content = filter = viewport = null;
    query = "";
    trigger.setAttribute("aria-expanded", "false");
    trigger.removeAttribute("aria-controls");
    if (refocus) trigger.focus();
  }

  function choose(o) {
    if (!o) return;
    const changed = o.value !== value;
    value = o.value;
    renderValue();
    closeList(true);
    if (changed) root.dispatchEvent(new Event("change", { bubbles: true }));
  }

  function onDocDown(e) {
    if (!root.contains(e.target)) closeList(false);
  }

  function onFilterKey(e) {
    const n = items().length;
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      if (n) setActive((active + (e.key === "ArrowDown" ? 1 : -1) + n) % n, true);
    } else if (e.key === "Home" || e.key === "End") {
      e.preventDefault();
      setActive(e.key === "Home" ? 0 : n - 1, true);
    } else if (e.key === "Enter") {
      e.preventDefault();
      if (!e.isComposing) choose(shown()[active]);
    } else if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      closeList(true);
    } else if (e.key === "Tab") {
      closeList(false);
    }
  }

  trigger.addEventListener("click", () => (isOpen() ? closeList(false) : openList()));
  trigger.addEventListener("keydown", (e) => {
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      openList();
    }
  });
  root.addEventListener("focusout", (e) => {
    if (e.relatedTarget && !root.contains(e.relatedTarget)) closeList(false);
  });

  Object.defineProperty(root, "value", {
    get: () => value,
    set(v) {
      value = String(v);
      renderValue();
      if (isOpen()) renderList();
    },
  });
  root.setOptions = (list) => {
    options = list;
    renderValue();
    if (isOpen()) renderList();
  };
  return root;
}
for (const el of document.querySelectorAll("[data-pg-select]")) initPicker(el);

// ---- Island bridge -------------------------------------------------------

const shared = (window.__pgState = window.__pgState || {});
function emit(partial) {
  Object.assign(shared, partial);
  window.dispatchEvent(new CustomEvent("pg:state", { detail: partial }));
}
let noteTimer = 0;
function note(text, ms = 4000) {
  emit({ note: text });
  clearTimeout(noteTimer);
  if (text) noteTimer = setTimeout(() => emit({ note: "" }), ms);
}

// ---- Small helpers -------------------------------------------------------

const enc = new TextEncoder();
const byteLen = (s) => (typeof s === "string" ? enc.encode(s).length : s.length);
const fmtBytes = (n) => (n < 1024 ? n + " B" : n < 1048576 ? (n / 1024).toFixed(1) + " KB" : (n / 1048576).toFixed(2) + " MB");
const escHTML = (s) => String(s).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");

async function streamBytes(input, stream) {
  const res = new Response(new Blob([input]).stream().pipeThrough(stream));
  return new Uint8Array(await res.arrayBuffer());
}
const gzipSize = async (text) => {
  if (typeof CompressionStream === "undefined") return -1;
  try {
    return (await streamBytes(typeof text === "string" ? enc.encode(text) : text, new CompressionStream("gzip"))).length;
  } catch {
    return -1;
  }
};
const toB64url = (bytes) => {
  let bin = "";
  for (let i = 0; i < bytes.length; i += 0x8000) bin += String.fromCharCode.apply(null, bytes.subarray(i, i + 0x8000));
  return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
};
const fromB64url = (s) => {
  const b = atob(s.replace(/-/g, "+").replace(/_/g, "/"));
  const out = new Uint8Array(b.length);
  for (let i = 0; i < b.length; i++) out[i] = b.charCodeAt(i);
  return out;
};

async function encodeShare(project) {
  const json = JSON.stringify({ v: 1, files: project.files, active: project.active, opts: project.opts });
  return toB64url(await streamBytes(enc.encode(json), new CompressionStream("deflate-raw")));
}
async function decodeShare(code) {
  const bytes = await streamBytes(fromB64url(code), new DecompressionStream("deflate-raw"));
  return sanitizeProject(JSON.parse(new TextDecoder().decode(bytes)));
}

const PATH_RE = /^src\/[A-Za-z0-9._@[\]()/-]+\.[A-Za-z0-9]+$/;
function validPath(p) {
  return typeof p === "string" && PATH_RE.test(p) && !p.includes("..") && !p.includes("//") && p.length < 120;
}
function sanitizeProject(raw) {
  if (!raw || typeof raw !== "object" || typeof raw.files !== "object" || raw.files === null) throw new Error("not a project");
  const files = {};
  let total = 0;
  for (const [k, v] of Object.entries(raw.files)) {
    if (!validPath(k) || typeof v !== "string") continue;
    total += v.length;
    if (total > MAX_TOTAL || Object.keys(files).length >= MAX_FILES) break;
    files[k] = v;
  }
  if (!Object.keys(files).length) throw new Error("no files");
  const active = typeof raw.active === "string" && raw.active in files ? raw.active : Object.keys(files)[0];
  const o = raw.opts && typeof raw.opts === "object" ? raw.opts : {};
  return { files, active, opts: { minify: !!o.minify, dev: !!o.dev } };
}

// ---- State ---------------------------------------------------------------

let examples = [];
const state = {
  files: {},
  active: "",
  exampleId: "",
  opts: { minify: false, dev: false },
  gated: false,
};
let saveTimer = 0;
function saveSoon() {
  clearTimeout(saveTimer);
  saveTimer = setTimeout(() => {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify({ files: state.files, active: state.active, exampleId: state.exampleId, opts: state.opts }));
    } catch {}
  }, 400);
}
const exampleById = (id) => examples.find((e) => e.id === id);
function isDirty() {
  const ex = exampleById(state.exampleId);
  return !ex || JSON.stringify(ex.files) !== JSON.stringify(state.files);
}

// ---- Tabs ----------------------------------------------------------------

function wireTablist(list, select) {
  list.addEventListener("keydown", (e) => {
    const tabs = [...list.querySelectorAll('[role="tab"]')];
    const i = tabs.indexOf(document.activeElement);
    if (i < 0) return;
    let next = -1;
    if (e.key === "ArrowRight") next = (i + 1) % tabs.length;
    else if (e.key === "ArrowLeft") next = (i - 1 + tabs.length) % tabs.length;
    else if (e.key === "Home") next = 0;
    else if (e.key === "End") next = tabs.length - 1;
    else return;
    e.preventDefault();
    tabs[next].focus();
    select(tabs[next]);
  });
  list.addEventListener("click", (e) => {
    const tab = e.target.closest('[role="tab"]');
    if (tab && list.contains(tab)) select(tab);
  });
}

// ---- Output state --------------------------------------------------------

let out = { files: {}, sizes: {}, gz: {} }; // last successful build
let diagnostics = [];
let lastLog = "";
let outTab = "preview";
const stale = new Set(["html", "js", "css", "files"]);
const picked = { html: "", js: "", css: "" };

function selectOutTab(name) {
  outTab = name;
  for (const t of $("pg-out-tabs").querySelectorAll('[role="tab"]')) {
    const on = t.dataset.tab === name;
    t.setAttribute("aria-selected", on ? "true" : "false");
    t.tabIndex = on ? 0 : -1;
    $("pg-panel-" + t.dataset.tab).hidden = !on;
  }
  renderOutTab(name);
}

function renderOutTab(name) {
  if (!stale.has(name)) return;
  stale.delete(name);
  if (name === "html" || name === "js" || name === "css") renderCode(name);
  else if (name === "files") renderFilesTab();
}

const outList = (ext) =>
  Object.keys(out.files)
    .filter((p) => p.endsWith("." + ext))
    .sort((a, b) => (a === "index.html" ? -1 : b === "index.html" ? 1 : a.localeCompare(b)));

function renderCode(kind) {
  const sel = $("pg-sel-" + kind);
  const pre = $("pg-code-" + kind);
  const list = outList(kind);
  if (!list.includes(picked[kind])) picked[kind] = list[0] || "";
  sel.setOptions(list.map((p) => ({ value: p })));
  sel.value = picked[kind];
  if (!list.length) {
    pre.innerHTML = '<span class="pg-empty">Nothing built yet.</span>';
    return;
  }
  let text = out.files[picked[kind]];
  const lang = langOf(picked[kind]);
  const pr = pretty(text, lang);
  let note = "";
  if (kind === "js" && text.length > 400 && text.split("\n").length < 4) note = "This output is minified. Turn off Minify to read it.";
  text = pr;
  let truncated = false;
  if (text.length > 400000) {
    text = text.slice(0, 400000);
    truncated = true;
  }
  const lines = highlightLines(text, lang);
  pre.innerHTML =
    (note ? `<span class="pg-note-line">${note}</span>` : "") +
    lines.map((h) => `<span class="l">${h}</span>`).join("") +
    (truncated ? '<span class="pg-note-line">Truncated.</span>' : "");
}

function markStale() {
  for (const n of ["html", "js", "css", "files"]) stale.add(n);
  renderOutTab(outTab);
}

function renderFilesTab() {
  const root = $("pg-files-out");
  const paths = Object.keys(out.files).sort((a, b) => a.localeCompare(b));
  if (!paths.length) {
    root.innerHTML = '<p class="pg-empty">Nothing built yet.</p>';
    return;
  }
  let rows = "";
  const seen = new Set();
  let total = 0;
  let totalGz = 0;
  for (const p of paths) {
    const parts = p.split("/");
    for (let d = 0; d < parts.length - 1; d++) {
      const dir = parts.slice(0, d + 1).join("/");
      if (seen.has(dir)) continue;
      seen.add(dir);
      rows += `<tr class="dir"><th scope="row" style="--depth:${d}">${escHTML(parts[d])}/</th><td></td><td></td></tr>`;
    }
    const size = out.sizes[p] ?? 0;
    const gz = out.gz[p];
    total += size;
    if (gz > 0) totalGz += gz;
    const ext = /\.(html|js|css)$/.exec(p)?.[1];
    const name = escHTML(parts[parts.length - 1]);
    const cell = ext ? `<button type="button" class="pg-linkbtn" data-open="${escHTML(p)}" data-kind="${ext}">${name}</button>` : name;
    rows += `<tr><th scope="row" style="--depth:${parts.length - 1}">${cell}</th><td>${fmtBytes(size)}</td><td>${gz > 0 ? fmtBytes(gz) : gz === undefined ? "..." : "-"}</td></tr>`;
  }
  rows += `<tr class="total"><th scope="row">${paths.length} files</th><td>${fmtBytes(total)}</td><td>${totalGz ? fmtBytes(totalGz) : ""}</td></tr>`;
  root.innerHTML = `<table class="pg-table"><thead><tr><th scope="col">File</th><th scope="col">Size</th><th scope="col">Gzip</th></tr></thead><tbody>${rows}</tbody></table>`;
}

$("pg-files-out").addEventListener("click", (e) => {
  const b = e.target.closest("[data-open]");
  if (!b) return;
  picked[b.dataset.kind] = b.dataset.open;
  stale.add(b.dataset.kind);
  selectOutTab(b.dataset.kind);
});
for (const k of ["html", "js", "css"]) {
  $("pg-sel-" + k).addEventListener("change", (e) => {
    picked[k] = e.target.value;
    stale.add(k);
    renderOutTab(k);
  });
}

// ---- Diagnostics -----------------------------------------------------------

function resolveProjectFile(file) {
  if (!file) return "";
  const f = file.replace(/\\/g, "/");
  const keys = Object.keys(state.files);
  return keys.find((k) => f === k || f.endsWith("/" + k)) || "";
}

function renderDiagnostics() {
  const root = $("pg-diags");
  if (!diagnostics.length) {
    root.innerHTML = '<p class="pg-empty">No diagnostics. The last build was clean.</p>' + logDetails();
    return;
  }
  root.innerHTML =
    '<ul class="pg-diaglist">' +
    diagnostics
      .map((d, i) => {
        const proj = resolveProjectFile(d.file);
        const loc = d.file ? `${d.file}${d.line > 0 ? ":" + d.line + (d.col > 0 ? ":" + d.col : "") : ""}` : "";
        const where = proj && d.line > 0
          ? `<button type="button" class="pg-linkbtn pg-diag-loc" data-diag="${i}">${escHTML(loc)}</button>`
          : loc
            ? `<span class="pg-diag-loc">${escHTML(loc)}</span>`
            : "";
        return (
          `<li class="pg-diag pg-diag-${d.severity === "error" ? "error" : "warning"}">` +
          `<span class="pg-sev">${d.severity === "error" ? "Error" : "Warning"}</span> ${where}` +
          `<p class="pg-diag-msg">${escHTML(d.message)}${d.code ? ` <code>${escHTML(d.code)}</code>` : ""}</p>` +
          (d.source ? `<pre class="pg-diag-src">${escHTML(d.source)}</pre>` : "") +
          (d.hint ? `<p class="pg-diag-hint">${escHTML(d.hint)}</p>` : "") +
          `</li>`
        );
      })
      .join("") +
    "</ul>" +
    logDetails();
}
const logDetails = () => (lastLog.trim() ? `<details class="pg-log-details"><summary>Compiler log</summary><pre>${escHTML(lastLog)}</pre></details>` : "");

$("pg-diags").addEventListener("click", (e) => {
  const b = e.target.closest("[data-diag]");
  if (!b) return;
  const d = diagnostics[Number(b.dataset.diag)];
  const proj = resolveProjectFile(d.file);
  if (!proj) return;
  openFile(proj);
  editor.goto(d.line, d.col);
});

function updateMarks() {
  editor.setMarks(
    diagnostics
      .filter((d) => d.line > 0 && resolveProjectFile(d.file) === state.active)
      .map((d) => ({ line: d.line, col: d.col, severity: d.severity === "error" ? "error" : "warning", message: d.message })),
  );
}

// ---- Console ---------------------------------------------------------------

let logCount = 0;
function addLog(level, text) {
  const root = $("pg-console");
  if (logCount === 0) root.textContent = "";
  logCount++;
  const row = document.createElement("div");
  row.className = "pg-logline pg-logline-" + level;
  const label = document.createElement("span");
  label.className = "pg-loglevel";
  label.textContent = level === "log" ? "" : level;
  const body = document.createElement("span");
  body.textContent = text;
  row.append(label, body);
  root.append(row);
  while (root.childNodes.length > 500) root.firstChild.remove();
  root.scrollTop = root.scrollHeight;
  emit({ logs: logCount });
}
function clearLogs() {
  logCount = 0;
  $("pg-console").innerHTML = '<p class="pg-empty">console.log output from the preview appears here.</p>';
  emit({ logs: 0 });
}
$("pg-clear").addEventListener("click", clearLogs);

// ---- Preview ---------------------------------------------------------------

const sid = Array.from(crypto.getRandomValues(new Uint8Array(10)), (b) => (b % 36).toString(36)).join("");
const PREVIEW_ROOT = "/playground/preview/" + sid;
let swReg = null;
let swFailed = "";
let previewPath = "/";
let previewReady = false; // files pushed to the worker at least once
let frames = $("pg-frames");
let currentFrame = null;
let frameCount = 0;

async function ensureWorker() {
  if (swReg?.active) return swReg.active;
  if (!("serviceWorker" in navigator)) throw new Error("Service workers are not available in this browser (or this context is not secure).");
  swReg = swReg || (await navigator.serviceWorker.register("/playground/preview/sw.js", { scope: "/playground/preview/" }));
  if (swReg.active) return swReg.active;
  const worker = swReg.installing || swReg.waiting;
  await new Promise((resolve, reject) => {
    const t = setTimeout(() => reject(new Error("The preview service worker did not start.")), 15000);
    const check = () => {
      if (swReg.active) {
        clearTimeout(t);
        resolve();
      } else if (worker.state === "redundant") {
        clearTimeout(t);
        reject(new Error("The preview service worker failed to install."));
      }
    };
    worker.addEventListener("statechange", check);
    check();
  });
  return swReg.active;
}

async function pushFiles(files) {
  const worker = await ensureWorker();
  await new Promise((resolve, reject) => {
    const ch = new MessageChannel();
    const t = setTimeout(() => reject(new Error("The preview service worker did not answer.")), 8000);
    ch.port1.onmessage = (e) => {
      clearTimeout(t);
      e.data?.ok ? resolve() : reject(new Error(e.data?.error || "update failed"));
    };
    worker.postMessage({ type: "update", sid, files }, [ch.port2]);
  });
  previewReady = true;
}

function setPreviewMessage(html) {
  let el = frames.querySelector(".pg-previewmsg");
  if (!html) {
    el?.remove();
    return;
  }
  if (!el) {
    el = document.createElement("div");
    el.className = "pg-previewmsg";
    frames.append(el);
  }
  el.innerHTML = html;
}

function loadFrame(path) {
  const iframe = document.createElement("iframe");
  iframe.className = "pg-frame pg-frame-incoming";
  iframe.title = "Preview";
  iframe.setAttribute("allow", "clipboard-write");
  const old = currentFrame;
  iframe.addEventListener("load", () => {
    if (old && old.isConnected) {
      try {
        const a = old.contentWindow;
        const b = iframe.contentWindow;
        if (a && b) b.scrollTo(a.scrollX, a.scrollY);
      } catch {}
    }
    iframe.classList.remove("pg-frame-incoming");
    old?.remove();
    $("pg").dataset.frames = String(++frameCount);
  });
  frames.append(iframe);
  currentFrame = iframe;
  iframe.src = PREVIEW_ROOT + (path.startsWith("/") ? path : "/" + path);
  setPreviewMessage("");
}

async function refreshPreview(reload = true) {
  if (state.gated || !Object.keys(out.files).length) return;
  try {
    swFailed = "";
    clearLogs();
    await pushFiles(out.files);
    if (reload) loadFrame(previewPath);
  } catch (err) {
    swFailed = String(err.message || err);
    setPreviewMessage(`<div class="pg-card"><h2>The preview could not start</h2><p>${escHTML(swFailed)}</p><p>The preview runs the built site through a service worker. You can still read the generated files in the other tabs.</p></div>`);
  }
}

window.addEventListener("message", (e) => {
  if (e.origin !== location.origin) return;
  const d = e.data;
  if (!d || d.pg !== 1 || d.sid !== sid) return;
  if (!currentFrame || e.source !== currentFrame.contentWindow) {
    const incoming = [...frames.querySelectorAll("iframe")].some((f) => f.contentWindow === e.source);
    if (!incoming) return;
  }
  if (d.type === "console") addLog(d.level, d.text);
  else if (d.type === "nav") {
    previewPath = d.path || "/";
    $("pg-url").textContent = previewPath;
    $("pg-popout").href = PREVIEW_ROOT + previewPath;
  }
});

$("pg-reload").addEventListener("click", () => refreshPreview(true));
$("pg-gate-run").addEventListener("click", () => {
  state.gated = false;
  $("pg-gate").hidden = true;
  refreshPreview(true);
});
$("pg-popout").addEventListener("click", (e) => {
  if (!previewReady) e.preventDefault();
});

// ---- Engine ----------------------------------------------------------------

let worker = null;
let nextId = 1;
const pending = new Map();
let engineReady = false;
let engineStarting = null;
let version = "";

function failAll(err) {
  for (const p of pending.values()) p.reject(err);
  pending.clear();
}

function spawnWorker() {
  const w = new Worker("/playground/engine.js", { type: "module" });
  w.onmessage = (e) => {
    const m = e.data;
    if (m.type === "progress") {
      emit({ stage: m.stage, loaded: m.loaded ?? shared.loaded ?? 0, total: m.total ?? shared.total ?? 0 });
      return;
    }
    const p = pending.get(m.id);
    if (!p) return;
    pending.delete(m.id);
    if (m.ok) p.resolve(m.result);
    else p.reject(Object.assign(new Error(m.error?.message || "engine error"), { stack: m.error?.stack }));
  };
  w.onerror = (e) => {
    engineReady = false;
    worker = null;
    failAll(new Error(e.message || "The compiler worker crashed."));
  };
  w.onmessageerror = () => {};
  return w;
}

function request(msg) {
  return new Promise((resolve, reject) => {
    const id = nextId++;
    pending.set(id, { resolve, reject });
    worker.postMessage({ id, ...msg });
  });
}

function startEngine() {
  if (engineStarting) return engineStarting;
  engineReady = false;
  worker?.terminate();
  worker = spawnWorker();
  emit({ stage: "manifest", loaded: 0, total: 0, message: "" });
  engineStarting = request({ type: "init" })
    .then((r) => {
      engineReady = true;
      version = r.version || "";
      emit({ stage: "ready", version });
      return r;
    })
    .catch((err) => {
      emit({ stage: "error", message: String(err.message || err) });
      throw err;
    })
    .finally(() => {
      engineStarting = null;
    });
  engineStarting.catch(() => {});
  return engineStarting;
}

async function engineAvailable() {
  try {
    const res = await fetch("/playground/manifest.json", { cache: "no-cache" });
    if (!res.ok) return false;
    const m = await res.json();
    return !!m && typeof m.wasm === "string";
  } catch {
    return false;
  }
}

// ---- Build -----------------------------------------------------------------

let building = false;
let dirty = false;
let buildTimer = 0;
let waitingForEngine = false;

function schedule(delay = 250) {
  clearTimeout(buildTimer);
  buildTimer = setTimeout(runBuild, delay);
}

async function runBuild() {
  clearTimeout(buildTimer);
  if (!engineReady) {
    waitingForEngine = true;
    if (!engineStarting && shared.stage !== "unavailable") startEngine().then(() => runBuild(), () => {});
    return;
  }
  if (building) {
    dirty = true;
    return;
  }
  building = true;
  try {
    do {
      dirty = false;
      await buildOnce();
    } while (dirty);
  } finally {
    building = false;
  }
}

async function buildOnce(retry = true) {
  const t0 = performance.now();
  let res;
  try {
    res = await request({ type: "build", files: { ...state.files }, opts: { minify: state.opts.minify, dev: state.opts.dev } });
  } catch (err) {
    if (retry && /exited|crashed|terminated/i.test(String(err.message))) {
      note("The compiler stopped; restarting it...", 6000);
      engineReady = false;
      try {
        await startEngine();
      } catch {
        return;
      }
      return buildOnce(false);
    }
    diagnostics = [{ severity: "error", message: "The compiler failed: " + err.message, file: "", line: 0, col: 0, hint: "", source: "", code: "" }];
    lastLog = String(err.stack || "");
    afterBuild(false, performance.now() - t0, null);
    return;
  }
  diagnostics = res.diagnostics || [];
  lastLog = res.log || "";
  if (!res.ok && !diagnostics.some((d) => d.severity === "error")) {
    diagnostics = [...diagnostics, { severity: "error", message: "The build failed.", file: "", line: 0, col: 0, hint: "See the compiler log below.", source: "", code: "" }];
  }
  const ms = res.timingsMs?.total ?? performance.now() - t0;
  afterBuild(res.ok, ms, res.ok ? res.files : null);
}

let buildCount = 0;
async function afterBuild(ok, ms, files) {
  $("pg").dataset.builds = String(++buildCount);
  const errors = diagnostics.filter((d) => d.severity === "error").length;
  const warnings = diagnostics.length - errors;
  renderDiagnostics();
  updateMarks();
  emit({ buildMs: ms, errors, warnings });
  if (!ok || !files) return;
  const sizes = {};
  for (const [p, t] of Object.entries(files)) sizes[p] = byteLen(t);
  out = { files, sizes, gz: {} };
  markStale();
  refreshPreview(true);
  // Gzip sizes for the Files tab and the status bar.
  const token = (afterBuild.token = (afterBuild.token || 0) + 1);
  const entries = Object.entries(files);
  const gz = await Promise.all(entries.map(([, t]) => gzipSize(t)));
  if (token !== afterBuild.token) return;
  let js = 0;
  entries.forEach(([p], i) => {
    out.gz[p] = gz[i];
    if (p.endsWith(".js") && gz[i] > 0) js += gz[i];
  });
  emit({ jsGz: js });
  stale.add("files");
  if (outTab === "files") renderOutTab("files");
}

// ---- Editor + files --------------------------------------------------------

const editor = createEditor($("pg-editor"), {
  onChange(value) {
    state.files[state.active] = value;
    saveSoon();
    schedule(250);
  },
  onRun: () => runBuild(),
  onSave: () => share(),
});

function renderFileTabs() {
  const list = $("pg-files");
  list.innerHTML = "";
  Object.keys(state.files).forEach((path, i) => {
    const b = document.createElement("button");
    b.type = "button";
    b.setAttribute("role", "tab");
    b.id = "pg-file-" + i;
    b.setAttribute("aria-controls", "pg-editor");
    b.setAttribute("aria-selected", path === state.active ? "true" : "false");
    b.tabIndex = path === state.active ? 0 : -1;
    b.title = path;
    b.textContent = path.replace(/^src\//, "");
    b.dataset.path = path;
    b.addEventListener("dblclick", () => startNameEdit("rename"));
    list.append(b);
    if (path === state.active) $("pg-editor").setAttribute("aria-labelledby", b.id);
  });
  $("pg-delete").disabled = Object.keys(state.files).length <= 1;
}

function openFile(path) {
  if (!(path in state.files)) return;
  state.active = path;
  editor.setFile(path, state.files[path]);
  renderFileTabs();
  updateMarks();
  saveSoon();
}

wireTablist($("pg-files"), (tab) => {
  openFile(tab.dataset.path);
  tab.focus();
});

let nameMode = "";
function startNameEdit(mode) {
  nameMode = mode;
  $("pg-name-form").hidden = false;
  $("pg-tabtools").hidden = true;
  $("pg-files").hidden = true;
  const input = $("pg-name");
  input.value = mode === "rename" ? state.active.replace(/^src\//, "") : "";
  input.focus();
  input.select();
}
function endNameEdit() {
  nameMode = "";
  $("pg-name-form").hidden = true;
  $("pg-tabtools").hidden = false;
  $("pg-files").hidden = false;
}

function normalizePath(raw) {
  let p = String(raw).trim().replace(/\\/g, "/").replace(/^\.?\//, "");
  if (!p) return "";
  if (!p.startsWith("src/")) p = "src/" + p;
  return p;
}

const TEMPLATES = {
  tsx: (name) => {
    const base = name.split("/").pop().replace(/\.[^.]+$/, "");
    const comp = base.replace(/(^|[-_.[\]])(\w)/g, (_, __, c) => c.toUpperCase()).replace(/[^A-Za-z0-9]/g, "") || "Component";
    return name.includes("/pages/")
      ? `export default function ${comp}() {\n  return <main>\n    <h1>${comp}</h1>\n  </main>;\n}\n`
      : `export function ${comp}(props: { children?: any }) {\n  return <div>{props.children}</div>;\n}\n`;
  },
  md: () => "---\ntitle: New page\n---\n\n# New page\n",
  css: () => "",
};

$("pg-name-form").addEventListener("submit", (e) => {
  e.preventDefault();
  const path = normalizePath($("pg-name").value);
  if (!validPath(path)) {
    note("Use a path like components/card.tsx (letters, numbers, - _ . /).");
    return;
  }
  if (nameMode === "add") {
    if (path in state.files) return note("That file already exists.");
    if (Object.keys(state.files).length >= MAX_FILES) return note("Too many files.");
    const ext = /\.([a-z0-9]+)$/i.exec(path)?.[1]?.toLowerCase();
    state.files[path] = (TEMPLATES[ext] || (() => ""))(path);
    endNameEdit();
    openFile(path);
    schedule(100);
  } else if (nameMode === "rename") {
    if (path !== state.active && path in state.files) return note("That file already exists.");
    const next = {};
    for (const [k, v] of Object.entries(state.files)) next[k === state.active ? path : k] = v;
    state.files = next;
    state.active = path;
    endNameEdit();
    openFile(path);
    schedule(100);
  }
});
$("pg-name-cancel").addEventListener("click", () => {
  endNameEdit();
  editor.focus();
});
$("pg-name").addEventListener("keydown", (e) => {
  if (e.key === "Escape") {
    e.preventDefault();
    endNameEdit();
    editor.focus();
  }
});
$("pg-add").addEventListener("click", () => startNameEdit("add"));
$("pg-rename").addEventListener("click", () => startNameEdit("rename"));
$("pg-delete").addEventListener("click", () => {
  const keys = Object.keys(state.files);
  if (keys.length <= 1) return;
  if (state.files[state.active].trim() && !confirm(`Delete ${state.active}?`)) return;
  const i = keys.indexOf(state.active);
  delete state.files[state.active];
  const rest = Object.keys(state.files);
  openFile(rest[Math.min(i, rest.length - 1)]);
  schedule(100);
});
$("pg-files").addEventListener("keydown", (e) => {
  if (e.key === "F2") {
    e.preventDefault();
    startNameEdit("rename");
  }
});

// ---- Projects, examples, sharing ---------------------------------------------

function loadProject(p, { run = true } = {}) {
  state.files = { ...p.files };
  state.active = p.active in p.files ? p.active : Object.keys(p.files)[0];
  if (p.opts) state.opts = { ...p.opts };
  $("pg-minify").checked = state.opts.minify;
  $("pg-dev").checked = state.opts.dev;
  diagnostics = [];
  lastLog = "";
  renderDiagnostics();
  emit({ errors: 0, warnings: 0 });
  openFile(state.active);
  saveSoon();
  if (run) schedule(0);
}

function loadExample(id) {
  const ex = exampleById(id);
  if (!ex) return;
  state.exampleId = ex.id;
  $("pg-example").value = ex.id;
  previewPath = ex.entry || "/";
  loadProject({ files: ex.files, active: ex.open in ex.files ? ex.open : Object.keys(ex.files)[0] });
}

function fillExampleSelect() {
  const sel = $("pg-example");
  sel.setOptions([...examples.map((e) => ({ value: e.id, label: e.title })), { value: "", label: "Custom project", hidden: true }]);
  sel.value = state.exampleId;
}

$("pg-example").addEventListener("change", (e) => {
  const id = e.target.value;
  if (isDirty() && !confirm("Replace your changes with this example?")) {
    e.target.value = state.exampleId;
    return;
  }
  loadExample(id);
});
$("pg-reset").addEventListener("click", () => {
  if (isDirty() && !confirm("Discard your changes and restore the example?")) return;
  loadExample(state.exampleId || examples[0]?.id);
});
$("pg-run").addEventListener("click", () => runBuild());
$("pg-minify").addEventListener("change", (e) => {
  state.opts.minify = e.target.checked;
  saveSoon();
  schedule(0);
});
$("pg-dev").addEventListener("change", (e) => {
  state.opts.dev = e.target.checked;
  saveSoon();
  schedule(0);
});

async function share() {
  if (typeof CompressionStream === "undefined") return note("Sharing needs a newer browser.");
  try {
    const code = await encodeShare({ files: state.files, active: state.active, opts: state.opts });
    const url = `${location.origin}/playground/#code=${code}`;
    shared.shareUrl = url;
    try {
      await navigator.clipboard.writeText(url);
      note(`Link copied (${url.length > 2000 ? "long: " : ""}${fmtBytes(url.length)}).`);
    } catch {
      history.replaceState(null, "", `#code=${code}`);
      note("Copy the link from the address bar.");
    }
  } catch (err) {
    note("Could not create the link: " + err.message);
  }
}
$("pg-share").addEventListener("click", share);

// ---- Layout: split pane -------------------------------------------------------

const main = $("pg-main");
const splitEl = $("pg-split");
function setSplit(pct, save = true) {
  pct = Math.min(80, Math.max(20, pct));
  main.style.setProperty("--pg-split", pct + "%");
  splitEl.setAttribute("aria-valuenow", String(Math.round(pct)));
  if (save) {
    try {
      localStorage.setItem(SPLIT_KEY, String(pct));
    } catch {}
  }
}
splitEl.addEventListener("pointerdown", (e) => {
  e.preventDefault();
  splitEl.setPointerCapture(e.pointerId);
  main.classList.add("pg-dragging");
  const rect = main.getBoundingClientRect();
  const move = (ev) => setSplit(((ev.clientX - rect.left) / rect.width) * 100, false);
  const up = () => {
    splitEl.removeEventListener("pointermove", move);
    splitEl.removeEventListener("pointerup", up);
    splitEl.removeEventListener("pointercancel", up);
    main.classList.remove("pg-dragging");
    setSplit(Number(splitEl.getAttribute("aria-valuenow")));
  };
  splitEl.addEventListener("pointermove", move);
  splitEl.addEventListener("pointerup", up);
  splitEl.addEventListener("pointercancel", up);
});
splitEl.addEventListener("keydown", (e) => {
  const cur = Number(splitEl.getAttribute("aria-valuenow"));
  if (e.key === "ArrowLeft") setSplit(cur - 3);
  else if (e.key === "ArrowRight") setSplit(cur + 3);
  else if (e.key === "Home") setSplit(20);
  else if (e.key === "End") setSplit(80);
  else return;
  e.preventDefault();
});

// ---- Boot ------------------------------------------------------------------------

wireTablist($("pg-out-tabs"), (tab) => selectOutTab(tab.dataset.tab));
$("pg-boot").addEventListener("click", (e) => {
  if (e.target.closest("#pg-retry")) {
    startEngine().then(() => runBuild(), () => {});
  }
});

// Mod+Enter and Mod+S work from anywhere in the playground.
$("pg").addEventListener("keydown", (e) => {
  if (e.target === editor.textarea) return;
  const mod = e.ctrlKey || e.metaKey;
  if (mod && e.key === "Enter") {
    e.preventDefault();
    runBuild();
  } else if (mod && (e.key === "s" || e.key === "S")) {
    e.preventDefault();
    share();
  }
});

async function loadExamples() {
  try {
    const res = await fetch("/playground/examples.json");
    if (!res.ok) throw new Error("HTTP " + res.status);
    const data = await res.json();
    examples = data.examples;
    return data.defaultExampleId;
  } catch {
    examples = [
      {
        id: "blank",
        title: "Blank page",
        description: "",
        entry: "/",
        open: "src/pages/index.tsx",
        files: { "src/pages/index.tsx": 'export default function Home() {\n  return <h1>Hello</h1>;\n}\n' },
      },
    ];
    return "blank";
  }
}

async function boot() {
  try {
    const saved = localStorage.getItem(SPLIT_KEY);
    if (saved) setSplit(Number(saved), false);
  } catch {}
  clearLogs();
  renderDiagnostics();
  const defaultId = await loadExamples();

  // Initial project: shared link, example link, saved work, default example.
  const hash = new URLSearchParams(location.hash.replace(/^#/, ""));
  let started = false;
  if (hash.get("code")) {
    try {
      loadProject(await decodeShare(hash.get("code")), { run: false });
      state.exampleId = "";
      state.gated = true;
      $("pg-gate").hidden = false;
      started = true;
      note("Loaded a shared project. Review the code before you run it.", 8000);
    } catch {
      note("That share link could not be read.", 8000);
    }
  } else if (hash.get("example") && exampleById(hash.get("example"))) {
    fillExampleSelect();
    loadExample(hash.get("example"));
    started = true;
  }
  if (!started) {
    let restored = null;
    try {
      restored = sanitizeProject(JSON.parse(localStorage.getItem(STORAGE_KEY) || "null"));
      const meta = JSON.parse(localStorage.getItem(STORAGE_KEY));
      state.exampleId = exampleById(meta.exampleId) ? meta.exampleId : "";
    } catch {
      restored = null;
    }
    if (restored) loadProject(restored, { run: false });
    else {
      state.exampleId = exampleById(defaultId) ? defaultId : examples[0].id;
      const ex = exampleById(state.exampleId);
      previewPath = ex.entry || "/";
      loadProject({ files: ex.files, active: ex.open }, { run: false });
    }
  }
  if (hash.get("code") || hash.get("example")) history.replaceState(null, "", location.pathname + location.search);
  fillExampleSelect();
  if (state.exampleId) {
    const ex = exampleById(state.exampleId);
    if (!hash.get("code") && ex && !isDirty()) previewPath = ex.entry || "/";
  }

  selectOutTab("preview");
  if (!(await engineAvailable())) {
    emit({ stage: "unavailable" });
    return;
  }
  startEngine().then(() => runBuild(), () => {});
}

boot();
