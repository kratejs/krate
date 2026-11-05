// HTML-level helpers of the portable server handler: baked-shell splice markers,
// title swaps, out-of-order stream scripts and the no-JS flash notice. Ports of
// internal/build/serve.go, region_stream.go and serve_flash.go.

import { escapeHTML } from "./handler-routing.js";

/** One splice marker and its nested markers. */
export interface RegionBoundary {
  openStart: number;
  openEnd: number;
  /** Start of the closing marker (end of the baked inner content). */
  fbEnd: number;
  id: string;
  kind: "suspense" | "region";
  children: RegionBoundary[];
}

export interface RegionShell {
  shell: string;
  roots: RegionBoundary[];
  all: RegionBoundary[];
  /** False when the file had no markers or a mismatched marker pair. */
  ok: boolean;
}

/** Extracts the nested marker tree of a baked shell. */
export function parseRegionShell(shell: string): RegionShell {
  const e: RegionShell = { shell, roots: [], all: [], ok: false };
  const re = /<!--(suspense|region):([^>]+?)-->/g;
  const opens: RegExpExecArray[] = [];
  let m: RegExpExecArray | null;
  while ((m = re.exec(shell)) !== null) opens.push(m);
  if (opens.length === 0) return e;
  e.ok = true;
  const stack: RegionBoundary[] = [];
  for (const o of opens) {
    const kind = o[1] as "suspense" | "region";
    const id = o[2];
    const openEnd = o.index + o[0].length;
    const rel = shell.indexOf("<!--/" + kind + ":" + id + "-->", openEnd);
    if (rel < 0) {
      e.ok = false;
      e.roots = [];
      e.all = [];
      return e;
    }
    const b: RegionBoundary = { openStart: o.index, openEnd, fbEnd: rel, id, kind, children: [] };
    while (stack.length > 0 && stack[stack.length - 1].fbEnd <= b.openStart) stack.pop();
    if (stack.length > 0) stack[stack.length - 1].children.push(b);
    else e.roots.push(b);
    stack.push(b);
    e.all.push(b);
  }
  return e;
}

/** Sibling regions only: a coarse page region or nested boundaries stay ordered. */
export function canStreamOutOfOrder(all: RegionBoundary[]): boolean {
  if (all.length === 0) return false;
  return all.every((b) => b.id !== "page" && b.children.length === 0);
}

export function allSuspense(all: RegionBoundary[]): boolean {
  return all.every((b) => b.kind === "suspense");
}

/** Keeps document markup in the streamed prefix and defers page scripts to the tail. */
export function splitBodyScripts(shell: string): [string, string] {
  const lower = shell.toLowerCase();
  const body = lower.indexOf("<body");
  if (body >= 0) {
    const rel = lower.indexOf("<script", body);
    if (rel >= 0) return [shell.slice(0, rel), shell.slice(rel)];
  }
  const at = lower.lastIndexOf("</body>");
  if (at >= 0) return [shell.slice(0, at), shell.slice(at)];
  return [shell, ""];
}

/** Swaps the baked <title> content of a shell. */
export function replaceTitle(shell: string, title: string): string {
  const m = /<title[^>]*>[\s\S]*?<\/title>/i.exec(shell);
  if (!m) return shell;
  return shell.slice(0, m.index) + "<title>" + title + "</title>" + shell.slice(m.index + m[0].length);
}

function scriptJSON(v: string): string {
  return JSON.stringify(v)
    .replace(/</g, "\\u003c")
    .replace(/>/g, "\\u003e")
    .replace(/&/g, "\\u0026")
    .replace(/\u2028/g, "\\u2028")
    .replace(/\u2029/g, "\\u2029");
}

// The client half of a streamed region's <Head>: the head is already on the
// wire, so the tags are applied by script. A same-key meta/canonical replaces
// the one in the document; scripts are recreated so they run.
const HEAD_APPLY =
  'if(tt){var d=document.createElement("textarea");d.innerHTML=tt;document.title=d.value;}' +
  'if(hd){var ht=document.createElement("template");ht.innerHTML=hd;[].slice.call(ht.content.children).forEach(function(e){' +
  'var k=e.tagName,q="";' +
  'if(k==="META"){if(e.hasAttribute("name"))q=\'meta[name="\'+e.getAttribute("name")+\'"]\';else if(e.hasAttribute("http-equiv"))q=\'meta[http-equiv="\'+e.getAttribute("http-equiv")+\'"]\';}' +
  'else if(k==="LINK"&&(e.getAttribute("rel")||"").toLowerCase()==="canonical")q=\'link[rel="canonical"]\';' +
  'if(q){try{var o=document.head.querySelector(q);if(o)o.remove();}catch(x){}}' +
  'var n=e;if(k==="SCRIPT"){n=document.createElement("script");[].slice.call(e.attributes).forEach(function(a){n.setAttribute(a.name,a.value);});n.text=e.textContent;}' +
  'document.head.appendChild(n);});}';

/**
 * Inline script that inserts one region chunk at its marker pair. head and
 * title are the region's <Head> content, applied to the document head.
 */
export function regionReplaceScript(id: string, html: string, head = "", title = ""): string {
  const payload = scriptJSON(html);
  const headPart = head || title ? "var hd=" + scriptJSON(head) + ",tt=" + scriptJSON(title) + ";" + HEAD_APPLY : "";
  return (
    "<script data-krate-stream>(function(){var id=" +
    JSON.stringify(id) +
    ";var html=" +
    payload +
    ';var kinds=["suspense:","region:"];var open=null,kind="";var w=document.createTreeWalker(document.documentElement,NodeFilter.SHOW_COMMENT,null,false);while(w.nextNode()){var v=w.currentNode.nodeValue||"";for(var i=0;i<kinds.length;i++){if(v===kinds[i]+id){open=w.currentNode;kind=kinds[i];break;}}if(open)break;}if(!open)return;var close=null,n=open.nextSibling;while(n){if(n.nodeType===8&&n.nodeValue==="/"+kind+id){close=n;break;}n=n.nextSibling;}if(!close)return;var r=document.createRange();r.setStartAfter(open);r.setEndBefore(close);r.deleteContents();var t=document.createElement("template");t.innerHTML=html;r.insertNode(t.content);' + headPart + '})();</script>'
  );
}

/** Writes a shell, replacing each marker block's inner content with its region HTML. */
export function spliceShell(shell: string, roots: RegionBoundary[], regionHTML: ReadonlyMap<string, string>, emit: (chunk: string) => void): void {
  const writeSpan = (lo: number, hi: number, children: RegionBoundary[]) => {
    let cursor = lo;
    for (const b of children) {
      emit(shell.slice(cursor, b.openEnd));
      const html = regionHTML.get(b.id);
      if (html !== undefined) emit(html);
      else if (b.kind === "suspense") writeSpan(b.openEnd, b.fbEnd, b.children);
      cursor = b.fbEnd;
    }
    emit(shell.slice(cursor, hi));
  };
  writeSpan(0, shell.length, roots);
}

// Flash state (no-JS form feedback)

export const FLASH_COOKIE = "krate_flash";

export interface FlashPayload {
  a: string;
  r: unknown;
}

function b64urlDecode(s: string): string | null {
  try {
    const b64 = s.replace(/=+$/, "").replace(/-/g, "+").replace(/_/g, "/");
    const bin = atob(b64 + "=".repeat((4 - (b64.length % 4)) % 4));
    const bytes = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
    return new TextDecoder().decode(bytes);
  } catch {
    return null;
  }
}

/** Decodes the flash cookie of a Cookie header. */
export function requestFlash(cookieHeader: string | null): { raw: string; payload: FlashPayload } | null {
  if (!cookieHeader) return null;
  let raw = "";
  for (const part of cookieHeader.split(";")) {
    const i = part.indexOf("=");
    if (i <= 0) continue;
    if (part.slice(0, i).trim() === FLASH_COOKIE) {
      raw = part.slice(i + 1).trim();
      if (raw.startsWith('"') && raw.endsWith('"')) raw = raw.slice(1, -1);
      break;
    }
  }
  if (raw === "") return null;
  // A signed cookie is "<payload>.<signature>"; the portable handler has no
  // secret to verify with, so it reads the payload part.
  const dot = raw.indexOf(".");
  if (dot >= 0) raw = raw.slice(0, dot);
  const text = b64urlDecode(raw);
  if (text === null) return null;
  try {
    const p = JSON.parse(text) as FlashPayload;
    if (!p || typeof p.a !== "string" || p.a === "") return null;
    return { raw, payload: p };
  } catch {
    return null;
  }
}

/** The human-readable strings of an action's returned state, in a stable order. */
export function flashMessages(value: unknown): string[] {
  const out: string[] = [];
  const walk = (v: unknown, depth: number) => {
    if (depth > 3) return;
    if (typeof v === "string") {
      const s = v.trim();
      if (s !== "") out.push(s);
    } else if (Array.isArray(v)) {
      for (const x of v) walk(x, depth + 1);
    } else if (v && typeof v === "object") {
      const obj = v as Record<string, unknown>;
      for (const k of Object.keys(obj).sort()) {
        const x = obj[k];
        if (typeof x === "string" || Array.isArray(x) || (x && typeof x === "object")) walk(x, depth + 1);
      }
    }
  };
  walk(value, 0);
  return out;
}

function regexpQuote(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

/** Inserts the notice after the opening tag of the form that produced the flash. */
export function injectFlashNotice(page: string, cookieValue: string, p: FlashPayload): string | null {
  const msgs = flashMessages(p.r);
  if (msgs.length === 0) return null;
  const re = new RegExp("<form\\b[^>]*?\\baction\\s*=\\s*[\"']?[^\"'\\s>]*/__krate/action/" + regexpQuote(p.a) + "[^>]*>", "i");
  const m = re.exec(page);
  if (!m) return null;
  const end = m.index + m[0].length;
  let n = '<div class="krate-flash" role="alert" data-krate-action="' + escapeHTML(p.a) + '" data-krate-flash="' + escapeHTML(cookieValue) + '">';
  for (const msg of msgs) n += "<p>" + escapeHTML(msg) + "</p>";
  n += "</div>";
  return page.slice(0, end) + n + page.slice(end);
}
