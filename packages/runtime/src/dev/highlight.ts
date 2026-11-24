// A small single-line highlighter for code frames. It only has to look right
// for a few lines of TSX, TS, JS or CSS around an error; anything it does not
// know stays plain.

import { esc } from './util.js';

const KEYWORDS = new Set(
  (
    'import export from default return const let var function if else for while do switch case break continue new ' +
    'class extends await async typeof instanceof in of try catch finally throw yield void delete this null undefined ' +
    'true false as type interface enum package func go defer chan map struct range select'
  ).split(' '),
);

type Tok = [string, string];

const SCRIPT = new Set(['tsx', 'ts', 'jsx', 'js', 'go', 'mdx']);

function tokenize(src: string, lang: string): Tok[] {
  const toks: Tok[] = [];
  const jsx = lang === 'tsx' || lang === 'jsx' || lang === 'mdx' || lang === 'html' || lang === 'svg';
  const css = lang === 'css';
  const script = SCRIPT.has(lang);
  let i = 0;
  let prevWord = '';
  let inTag = false;
  const push = (text: string, cls: string) => {
    if (text) toks.push([text, cls]);
  };
  while (i < src.length) {
    const rest = src.slice(i);
    let m: RegExpExecArray | null;
    if ((script || css) && (m = /^(\/\/.*|\/\*.*?(?:\*\/|$))/.exec(rest)) && !(css && rest.startsWith('//'))) {
      push(m[0], 'k5');
    } else if ((m = /^("(?:\\.|[^"\\])*"?|'(?:\\.|[^'\\])*'?|`(?:\\.|[^`\\])*`?)/.exec(rest))) {
      push(m[0], 'k3');
    } else if (css && (m = /^#[0-9a-fA-F]{3,8}\b/.exec(rest))) {
      push(m[0], 'k1');
    } else if ((m = /^\d[\d_]*(?:\.\d+)?(?:[a-z%]+)?/.exec(rest)) && !/[\w$]/.test(src[i - 1] || '')) {
      push(m[0], 'k1');
    } else if (jsx && (m = /^<\/?(?=[A-Za-z])/.exec(rest))) {
      push(m[0], 'k5');
      const name = /^[A-Za-z][\w.:-]*/.exec(src.slice(i + m[0].length));
      if (name) {
        push(name[0], 'k1');
        i += name[0].length;
      }
      inTag = true;
    } else if (jsx && (m = /^\/?>/.exec(rest)) && src[i - 1] !== '=' && (inTag || /^\s*\/?>/.test(src.slice(0, i + m[0].length)))) {
      push(m[0], 'k5');
      inTag = false;
    } else if (jsx && (m = /^[A-Za-z_$][\w$]*(?:[-:][\w$]+)+(?=\s*=(?![=>]))/.exec(rest))) {
      push(m[0], 'k2');
    } else if (css && (m = /^[\w-]+(?=\s*:)/.exec(rest))) {
      push(m[0], 'k2');
    } else if ((m = /^[A-Za-z_$][\w$]*/.exec(rest))) {
      const w = m[0];
      const after = src.slice(i + w.length);
      const before = src[i - 1] || '';
      let cls = '';
      if (css) cls = '';
      else if (KEYWORDS.has(w) && before !== '.') cls = 'k4';
      else if (/^\s*\(/.test(after)) cls = 'k2';
      else if (/^\s*=(?![=>])/.test(after)) cls = prevWord === 'const' || prevWord === 'let' || prevWord === 'var' ? 'k6' : 'k2';
      else if (prevWord === 'const' || prevWord === 'let' || prevWord === 'var' || prevWord === 'function') cls = 'k6';
      else if (before === '{' || /^\s*[}]/.test(after)) cls = 'k6';
      push(w, cls);
      prevWord = w;
      i += w.length;
      continue;
    } else {
      push(src[i], '');
      i++;
      continue;
    }
    i += m[0].length;
  }
  return toks;
}

/** The [start, end) range of the word at a 1-based column, for the squiggle. */
export function wordAt(src: string, col: number): [number, number] {
  let s = Math.max(0, Math.min(src.length, col - 1));
  if (s >= src.length) return [Math.max(0, src.length - 1), src.length];
  const isWord = (c: string) => /[\w$-]/.test(c);
  if (!isWord(src[s])) return [s, s + 1];
  let e = s;
  while (s > 0 && isWord(src[s - 1])) s--;
  while (e < src.length && isWord(src[e])) e++;
  return [s, e];
}

/** One highlighted line as HTML; `sq` underlines [start, end) with a squiggle. */
export function highlightLine(src: string, lang: string, sq?: [number, number]): string {
  let out = '';
  let pos = 0;
  for (const [text, cls] of tokenize(src, lang)) {
    const end = pos + text.length;
    if (sq && sq[0] < end && sq[1] > pos) {
      const a = Math.max(sq[0], pos) - pos;
      const b = Math.min(sq[1], end) - pos;
      const k = cls ? cls + ' ' : '';
      if (a > 0) out += span(text.slice(0, a), cls);
      out += '<span class="' + k + 'sq">' + esc(text.slice(a, b)) + '</span>';
      if (b < text.length) out += span(text.slice(b), cls);
    } else {
      out += span(text, cls);
    }
    pos = end;
  }
  return out;
}

function span(text: string, cls: string): string {
  return cls ? '<span class="' + cls + '">' + esc(text) + '</span>' : esc(text);
}
