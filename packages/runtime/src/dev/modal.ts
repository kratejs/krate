// The error overlay: an issue rail, a pager and one issue in detail (build
// diagnostics, runtime errors, server crashes).

import type { KrateDev } from './app.js';
import type { FixChange, LogLine } from './api.js';
import { highlightLine, wordAt } from './highlight.js';
import { icon, mark } from './icons.js';
import { componentPath, readTable } from './inspect.js';
import { type CodeFrame, type Issue, copyForAI, issueSummary } from './issues.js';
import { type FailedRequest, NEAR_BEFORE_MS, displayURL, rawFetch } from './requests.js';
import { parseStack, splitStack } from './stack.js';
import { basename, clock, copyText, dirname, esc, plural, richText } from './util.js';

const KIND_LABEL: Record<Issue['kind'], string> = { build: 'Build', server: 'Server', runtime: 'Runtime' };

function frameKey(i: Issue): string {
  return (i.file || '') + ':' + (i.line || 0);
}

function fixKey(i: Issue): string {
  return (i.file || '') + '|' + (i.code || '');
}

function isProjectFile(f: string | undefined): f is string {
  return !!f && !/^[a-z][a-z0-9+.-]*:\/\//i.test(f) && !/^(chunks|__krate)\//.test(f);
}

function canOpen(i: Issue): boolean {
  return isProjectFile(i.file) && !i.served;
}

const SOURCE_NAME: Record<string, string> = {
  'SSR renderer': 'SSR sidecar',
  'Node API sidecar': 'API sidecar',
  'Go API sidecar': 'Go API sidecar',
};

function title(i: Issue, all: Issue[]): [string, string] {
  if (i.kind === 'build') {
    const errs = all.filter((x) => x.kind === 'build' && x.severity === 'error').length;
    return errs ? ['Build failed', 'red'] : ['Build warnings', 'amber'];
  }
  if (i.kind === 'server') {
    const src = i.server?.source || 'Server';
    const name = SOURCE_NAME[src] || src;
    const crashed = i.server?.exitCode !== undefined || /exited|crash/i.test(i.server?.detail || i.message);
    return [name + (crashed ? ' crashed' : ' failed'), 'red'];
  }
  switch (i.origin) {
    case 'console':
      return ['Console error', 'amber'];
    case 'resource':
      return ['Failed to load a resource', 'red'];
    case 'rejection':
      return ['Unhandled promise rejection', 'red'];
  }
  return ['Unhandled runtime error', 'red'];
}

/** The component instance whose source holds the error location, for the path crumbs. */
function componentAt(app: KrateDev, i: Issue): string[] {
  if (!i.file) return [];
  const rows = readTable() || [];
  const scope = app.frames.get(frameKey(i))?.scope;
  const byKey = scope ? rows.find((r) => r.key === scope) : undefined;
  if (byKey) return componentPath(rows, byKey);
  let best: (typeof rows)[number] | undefined;
  for (const r of rows) {
    if (!r.file || !(i.file.endsWith(r.file) || r.file.endsWith(i.file))) continue;
    if (r.line && i.line && r.line > i.line) continue;
    if (!best || (r.line || 0) > (best.line || 0)) best = r;
  }
  return best ? componentPath(rows, best) : [];
}

function sub(app: KrateDev, i: Issue, list: Issue[]): string {
  const t = clock(i.time);
  if (i.kind === 'build') return issueSummary(list.filter((x) => x.kind === 'build')) + ' \u00B7 ' + t;
  if (i.kind === 'server') return serverSub(i, t);
  const path = componentAt(app, i);
  return 'in the browser' + (path.length ? ' while rendering ' + path[path.length - 1] : '') + ' \u00B7 ' + t;
}

const TIMES = ['', 'once', 'twice'];

/** "node 22.11 · exited with code 1 · 12:44:30 · restarted twice in the last minute". */
export function serverSub(i: Issue, t: string): string {
  const s = i.server;
  const parts: string[] = [];
  if (s?.runtime) parts.push(s.runtime);
  if (s?.exitCode !== undefined) parts.push('exited with code ' + s.exitCode);
  if (i.count > 1 && !s?.restarts) parts.push('reported ' + plural(i.count, 'time'));
  parts.push(t);
  const n = s?.restarts || 0;
  if (n > 0 && s?.restart) parts.push('restarted ' + (TIMES[n] || n + ' times') + ' in the last minute');
  return parts.join(' \u00B7 ');
}

function railHTML(app: KrateDev, list: Issue[]): string {
  let html = '';
  let last = '';
  list.forEach((i, n) => {
    if (i.kind !== last) {
      html += '<div class="lbl">' + KIND_LABEL[i.kind] + '</div>';
      last = i.kind;
    }
    html +=
      '<button class="issue ' +
      (i.severity === 'error' ? 'e' : 'w') +
      '" type="button" data-act="goto" data-n="' +
      n +
      '" aria-current="' +
      (n === app.cur) +
      '"><div class="top1"><span class="mono">' +
      esc(i.label) +
      '</span>' +
      (i.severity === 'warning' && i.kind === 'build' ? '<span>warning</span>' : '') +
      (i.count > 1 ? '<span class="cnt">x' + i.count + '</span>' : '') +
      '</div><div class="msg">' +
      richText(i.message.split('\n')[0]) +
      '</div>' +
      (i.file ? '<div class="file">' + esc(basename(i.file)) + (i.line ? ':' + i.line : '') + '</div>' : '') +
      '</button>';
  });
  return html;
}

function pathHTML(i: Issue, shown?: string): string {
  const f = shown || i.file || '';
  return (
    '<span class="path">' +
    esc(dirname(f)) +
    '<b>' +
    esc(basename(f)) +
    '</b>' +
    (i.line ? ':' + i.line + (i.col ? ':' + i.col : '') : '') +
    '</span>'
  );
}

/** The changed line of a fix: `from` replaced at its column, or its first occurrence. */
export function applyChange(line: string, c: FixChange): string {
  const at = c.col - 1;
  if (at >= 0 && line.slice(at, at + c.from.length) === c.from) return line.slice(0, at) + c.to + line.slice(at + c.from.length);
  const k = line.indexOf(c.from);
  return k === -1 ? line : line.slice(0, k) + c.to + line.slice(k + c.from.length);
}

export function codeFrameHTML(frame: CodeFrame | null, i: Issue, note = ''): string {
  let lines: string[] = [];
  let start = 1;
  let lang = 'text';
  let file = i.file || '';
  if (frame && frame.lines.length) {
    lines = frame.lines;
    start = frame.start;
    lang = frame.lang || 'text';
    file = frame.file || file;
  } else if (i.source) {
    lines = [i.source];
    start = i.line || 1;
    lang = /\.(tsx|ts|jsx|js|css|go|mdx)$/.exec(file)?.[1] || 'text';
  } else {
    return '';
  }
  let html =
    '<div class="cf' +
    (i.kind === 'build' && i.severity === 'warning' ? ' w' : '') +
    '"><div class="cf-bar"><span>' +
    esc(basename(file)) +
    '</span>' +
    (i.served ? '<span>built output, no source map</span>' : '') +
    '<span class="grow"></span><span>' +
    esc(lang.toUpperCase()) +
    '</span></div>';
  lines.forEach((text, k) => {
    const n = start + k;
    const hl = n === i.line;
    const sq = hl && i.col ? wordAt(text, i.col) : undefined;
    html +=
      '<div class="cf-l' +
      (hl ? ' hl' : '') +
      '"><span class="n">' +
      n +
      '</span><span class="c">' +
      highlightLine(text, lang, sq) +
      '</span></div>';
    if (hl && sq && note) {
      html +=
        '<div class="cf-l ann"><span class="n"></span><span class="c">' +
        esc(' '.repeat(sq[0]) + '^'.repeat(Math.max(1, sq[1] - sq[0])) + (note ? ' ' + note : '')) +
        '</span></div>';
    }
  });
  return html + '</div>';
}

function msgHTML(html: string, raw: string): string {
  return '<h3 class="d-msg' + (raw.length > 140 || raw.includes('\n') ? ' long' : '') + '" id="kd-msg">' + html + '</h3>';
}

function fixFor(app: KrateDev, i: Issue): FixChange | null {
  const list = app.fixes.get(fixKey(i));
  if (!list || !list.length) return null;
  return list.find((c) => c.line === i.line) || list[0];
}

function buildDetail(app: KrateDev, i: Issue, list: Issue[]): string {
  const frame = app.frames.get(frameKey(i)) || null;
  const fix = fixFor(app, i);
  let html = '<div class="d-meta">';
  const info = i.code ? app.codes.get(i.code) : undefined;
  if (i.code) {
    html += '<span class="code' + (i.severity === 'warning' ? ' w' : '') + '">' + esc(i.code) + '</span>';
    if (info) html += '<span class="chip">' + esc(info.title) + '</span>';
    html += info
      ? '<button class="whatbtn" type="button" data-act="code-pop" aria-expanded="' +
        (app.codePop === i.code) +
        '" aria-controls="kd-codepop">What this means</button>'
      : '<a class="link" style="font-size:12.5px;text-decoration:none" href="' +
        esc(app.docsURL(i.code)) +
        '" target="_blank" rel="noopener">What this means \u2197</a>';
  } else {
    html += '<span class="code' + (i.severity === 'warning' ? ' w' : '') + '">' + esc(i.label) + '</span>';
  }
  html += '</div>';
  if (i.code && info && app.codePop === i.code) {
    html +=
      '<div class="codepop" id="kd-codepop" role="dialog" aria-label="' +
      esc(i.code + ': ' + info.title) +
      '"><button class="iconbtn x" type="button" data-act="code-pop" aria-label="Close">' +
      icon('x') +
      '</button><div class="t">' +
      esc(i.code) +
      ' \u00B7 ' +
      esc(info.title) +
      '</div><div>' +
      richText(info.explanation) +
      '</div><a href="' +
      esc(app.docsURL(i.code)) +
      '" target="_blank" rel="noopener">Read the docs \u2197</a></div>';
  }
  html += msgHTML(richText(i.message), i.message);
  html += fileLine(app, i, frame?.file);
  html += codeFrameHTML(frame, i, fix ? 'expected ' + fix.to : '');
  if (i.hint) html += '<div class="hint">' + icon('lightbulb') + '<div>' + richText(i.hint) + '</div></div>';
  if (fix) {
    const fixable = list.filter((x) => x.kind === 'build' && x.code).length;
    const src = frame?.lines[fix.line - frame.start] ?? (fix.line === i.line ? i.source : undefined);
    const w = String(fix.line).length;
    const pad = (s: string) => s + ' '.repeat(Math.max(1, 8 - w));
    const busy = app.busy.has('fix');
    html +=
      '<div class="fix"><div class="fix-h">' +
      icon('wand-sparkles') +
      '<span class="t">Safe fix available</span><span class="s">same rewrite as <span class="mono">krate fix</span></span><span class="grow"></span>' +
      (fixable > 1 ? '<button class="b ghost" type="button" data-act="fix-all"' + (busy ? ' disabled' : '') + '>Fix all ' + fixable + '</button>' : '') +
      '<button class="b pri" type="button" data-act="fix"' +
      (busy ? ' disabled' : '') +
      '>' +
      (busy ? 'Applying' : 'Apply fix') +
      ' <span class="kbd">F</span></button></div>' +
      (src !== undefined
        ? '<div class="diff"><div class="del">' +
          esc('- ' + pad(String(fix.line)) + src) +
          '</div><div class="add">' +
          esc('+ ' + pad(String(fix.line)) + applyChange(src, fix)) +
          '</div></div>'
        : '<div class="diff"><div class="del">' +
          esc('- ' + fix.from) +
          '</div><div class="add">' +
          esc('+ ' + fix.to) +
          '</div></div>') +
      '</div>';
  }
  return html;
}

function fileLine(app: KrateDev, i: Issue, shown?: string): string {
  if (!i.file) {
    return (
      '<div class="fileline"><span class="grow"></span><button class="b" type="button" data-act="copy-ai">' +
      icon('copy') +
      'Copy for AI</button></div>'
    );
  }
  return (
    '<div class="fileline">' +
    pathHTML(i, shown) +
    '<span class="grow"></span>' +
    (canOpen(i)
      ? '<button class="b" type="button" data-act="open-issue">' + icon('square-arrow-up-right-exit') + 'Open in editor <span class="kbd">O</span></button>'
      : '') +
    '<button class="b" type="button" data-act="copy-ai">' +
    icon('copy') +
    'Copy for AI</button></div>'
  );
}

function runtimeDetail(app: KrateDev, i: Issue): string {
  const frame = canOpen(i) || i.served ? app.frames.get(frameKey(i)) || null : null;
  const frames = parseStack(i.stack);
  const items = splitStack(frames);
  const appCount = frames.filter((f) => f.app).length;
  const path = componentAt(app, i);
  let html = '<div class="d-meta"><span class="code' + (i.origin === 'console' ? ' w' : '') + '">' + esc(i.label) + '</span>';
  if (path.length) html += '<span class="chip">client island</span>';
  if (i.origin === 'remote') html += '<span class="chip">another tab</span>';
  if (i.count > 1) html += '<span class="chip">happened ' + i.count + ' times</span>';
  html += '</div>' + msgHTML(esc(i.message), i.message);
  if (path.length > 1) {
    html +=
      '<div class="crumbs">' +
      path
        .map((n, k) => '<span' + (k === path.length - 1 ? ' class="cur"' : '') + '>&lt;' + esc(n) + '&gt;</span>')
        .join('<span class="sepc">\u203A</span>') +
      '</div>';
  }
  html += fileLine(app, i, frame?.file);
  html += codeFrameHTML(frame, i);
  const console = app.localLogs.slice(-30);
  const reqs = app.requests.near(i.time);
  const tab = app.runtimeTab === 'requests' && !reqs.length ? 'stack' : app.runtimeTab;
  html +=
    '<div class="subtabs" role="tablist"><button type="button" role="tab" data-act="rt-tab" data-v="stack" aria-selected="' +
    (tab === 'stack') +
    '">Stack' +
    (frames.length ? '<span>' + appCount + ' of ' + frames.length + '</span>' : '') +
    '</button><button type="button" role="tab" data-act="rt-tab" data-v="console" aria-selected="' +
    (tab === 'console') +
    '">Console<span>' +
    console.length +
    '</span></button>' +
    (reqs.length
      ? '<button type="button" role="tab" data-act="rt-tab" data-v="requests" aria-selected="' +
        (tab === 'requests') +
        '">Requests<span>' +
        reqs.length +
        '</span></button>'
      : '') +
    '</div>';
  if (tab === 'requests') return html + requestsHTML(reqs, i.time);
  if (tab === 'console') {
    html += console.length
      ? '<div class="term">' +
        console
          .map(
            (l) =>
              '<div class="tl"><span class="ts">' +
              clock(l.t) +
              '</span><span class="' +
              (l.lvl === 'error' ? 'e' : l.lvl === 'warn' ? 'w' : 'm') +
              '">' +
              esc(l.text) +
              '</span></div>',
          )
          .join('') +
        '</div>'
      : '<div class="empty">Nothing was logged in this tab.</div>';
    return html;
  }
  if (!frames.length) {
    html += i.stack ? '<div class="frames"><pre class="stackraw">' + esc(i.stack) + '</pre></div>' : '<div class="empty">No stack trace.</div>';
  } else {
    html += '<div class="frames">';
    items.forEach((it, k) => {
      if (it.type === 'app') {
        const f = it.frame;
        html +=
          (f.served
            ? '<div class="fr app">'
            : '<button class="fr app" type="button" data-act="open" data-file="' + esc(f.file) + '" data-line="' + f.line + '" data-col="' + f.col + '">') +
          '<span class="fn">' +
          esc(f.fn) +
          '</span><span class="at"><b>' +
          esc(f.file) +
          '</b>:' +
          f.line +
          ':' +
          f.col +
          '</span>' +
          (f.served ? '</div>' : '</button>');
        return;
      }
      const key = i.id + '#' + k;
      const open = app.unfold.has(key);
      html +=
        '<button class="fr fold" type="button" data-act="unfold" data-k="' +
        esc(key) +
        '"><span>' +
        plural(it.frames.length, 'framework frame') +
        frameworkOrigin(it.frames) +
        '</span><span class="link">' +
        (open ? 'Hide' : 'Show') +
        '</span></button>';
      if (open) {
        for (const f of it.frames) {
          html +=
            '<div class="fr fw"><span class="fn">' +
            esc(f.fn) +
            '</span><span class="at">' +
            esc(f.file) +
            ':' +
            f.line +
            ':' +
            f.col +
            '</span></div>';
        }
      }
    });
    html += '</div>';
  }
  if (i.hostStack) html += '<div class="lbl" style="margin-top:14px">Host stack</div><div class="frames"><pre class="stackraw">' + esc(i.hostStack) + '</pre></div>';
  return html;
}

/** Failed requests around a runtime error, newest last. */
export function requestsHTML(reqs: FailedRequest[], at: number): string {
  let html = '<div class="reqs" role="list">';
  for (const r of reqs) {
    const delta = Math.round((r.t - at) / 100) / 10;
    html +=
      '<div class="req" role="listitem"><span class="st">' +
      esc(r.status ? String(r.status) : 'failed') +
      '</span><span class="m">' +
      esc(r.method) +
      '</span><span class="u" title="' +
      esc(r.url) +
      '">' +
      esc(displayURL(r.url)) +
      '</span><span class="ms">' +
      esc(r.ms + ' ms') +
      '</span><span class="ts" title="' +
      esc(clock(r.t)) +
      '">' +
      esc((delta > 0 ? '+' : '') + delta + ' s') +
      '</span>' +
      (r.error ? '<span class="why">' + esc(r.error) + '</span>' : '') +
      '</div>';
  }
  return (
    html +
    '</div><div class="empty" style="text-align:left;padding:8px 2px 0">Failed fetch and XHR requests of this page from ' +
    NEAR_BEFORE_MS / 1000 +
    ' s before the error; times are relative to it.</div>'
  );
}

function frameworkOrigin(frames: { file: string }[]): string {
  if (frames.every((f) => /node_modules/.test(f.file))) return ' from node_modules';
  if (frames.every((f) => /runtime|krate-|chunks\/runtime/.test(f.file))) return ' from @krate/runtime';
  return '';
}

function logLineClass(text: string): string {
  if (/\b(error|failed|panic|fatal|exception|uncaught|unhandled)\b|[A-Z]\w*Error\b/i.test(text)) return 'e';
  if (/\bwarn(ing)?\b|deprecat/i.test(text)) return 'w';
  return 'm';
}

function serverDetail(app: KrateDev, i: Issue): string {
  const s = i.server || { message: i.message };
  const frame = isProjectFile(i.file) ? app.frames.get(frameKey(i)) || null : null;
  let html = '<div class="d-meta"><span class="code">' + esc(i.label) + '</span>';
  if (s.source && s.source !== i.label) html += '<span class="chip">' + esc(s.source) + '</span>';
  html += '</div>' + msgHTML(richText(i.message), i.message);
  if (i.file) html += fileLine(app, i, frame?.file) + codeFrameHTML(frame, i);
  const tail = app.tails.get(i.id);
  const log = tail && tail.length ? tail.map((l) => l.text) : s.log && s.log.length ? s.log : [];
  const lastErr = log.reduce((a, l, k) => ((tail && tail[k] ? tail[k].lvl === 'error' : logLineClass(l) === 'e') ? k : a), -1);
  const logFile = app.logFile();
  html +=
    '<div class="lbl" style="margin-top:18px">Last console lines<span class="grow"></span>' +
    (logFile ? '<button class="link" type="button" style="font-size:12px" data-act="open" data-file="' + esc(logFile) + '">Open ' + esc(logFile) + '</button>' : '') +
    '</div>';
  if (log.length) {
    html +=
      '<div class="term">' +
      log
        .map((l, k) => {
          const t = tail && tail[k];
          return (
            '<div class="tl' +
            (k === lastErr ? ' hl' : '') +
            '">' +
            (t ? '<span class="ts">' + clock(t.t) + '</span><span class="src s-' + esc(t.src) + '">' + esc(t.src) + '</span>' : '') +
            '<span class="' +
            (t && t.lvl === 'error' ? 'e' : t && t.lvl === 'warn' ? 'w' : logLineClass(l)) +
            '">' +
            esc(t ? l.replace(/^\s+/, '') : l) +
            '</span></div>'
          );
        })
        .join('') +
      '</div>';
  } else if (s.stack) {
    html += '<div class="term"><pre class="stackraw">' + esc(s.stack) + '</pre></div>';
  } else {
    html += '<div class="empty">No console output was captured.</div>';
  }
  return html;
}

function footer(app: KrateDev, i: Issue): string {
  if (i.kind === 'build') {
    const failed = app.visibleIssues().some((x) => x.kind === 'build' && x.severity === 'error');
    if (!failed) {
      return (
        '<span class="sp">' +
        (app.busy.has('fixed') ? '<span class="spin"></span>Fix applied, rebuilding.' : 'The build passed with warnings. They clear when the code is fixed.') +
        '</span><span class="grow"></span>' +
        '<button class="b ghost" type="button" data-act="ignore">Ignore this warning for the session</button>'
      );
    }
    // A built page on screen is the last good build; the error shell is not.
    const lastGood = !!app.lastGoodAt && !!app.cfg.page;
    return (
      '<span class="sp"><span class="spin"></span>' +
      (app.busy.has('fixed') ? 'Fix applied, rebuilding.' : 'Watching for changes. This closes when the build passes.') +
      '</span><span class="grow"></span>' +
      (app.lastGoodAt ? '<span>Last good build ' + clock(app.lastGoodAt) + '</span>' : '') +
      (lastGood ? '<button class="link" type="button" data-act="last-good">Show last good page</button>' : '')
    );
  }
  if (i.kind === 'server') {
    const r = i.server?.restart;
    const busy = app.busy.has('restart');
    return (
      '<span class="sp">' +
      (busy ? '<span class="spin"></span>Restarting.' : r ? 'Restart it here, or save a file.' : '<span class="spin"></span>Retries on the next request or save.') +
      '</span><span class="grow"></span>' +
      (i.server?.log?.length ? '<button class="b ghost" type="button" data-act="copy-log">Copy log</button>' : '') +
      '<button class="b ghost" type="button" data-act="copy-ai">Copy for AI</button>' +
      (r && app.cfg.api
        ? '<button class="b pri" type="button" data-act="restart"' + (busy ? ' disabled' : '') + '>Restart sidecar</button>'
        : '')
    );
  }
  return (
    '<span>Reported to the terminal as well.</span><span class="grow"></span>' +
    '<button class="b ghost" type="button" data-act="ignore">Ignore this error for the session</button>' +
    '<button class="b" type="button" data-act="reload">Reload page <span class="kbd">R</span></button>'
  );
}

export function renderModal(app: KrateDev): void {
  const box = app.modal;
  if (!box) return;
  const list = app.visibleIssues();
  if (!list.length) {
    box.hidden = true;
    return;
  }
  if (app.cur >= list.length) app.cur = list.length - 1;
  const i = list[app.cur];
  const [t, color] = title(i, list);
  const detail = i.kind === 'build' ? buildDetail(app, i, list) : i.kind === 'server' ? serverDetail(app, i) : runtimeDetail(app, i);
  const scroll = box.querySelector('.detail')?.scrollTop || 0;
  const same = box.getAttribute('data-issue') === i.id;
  box.setAttribute('data-issue', i.id);
  box.innerHTML =
    '<div class="modal" role="dialog" aria-modal="true" aria-labelledby="kd-title" aria-describedby="kd-msg" tabindex="-1"><div class="m-head' +
    (color === 'amber' ? ' warn' : '') +
    '">' +
    mark() +
    '<div><div class="m-title ' +
    color +
    '" id="kd-title">' +
    esc(t) +
    '</div><div class="m-sub">' +
    esc(sub(app, i, list)) +
    '</div></div><span class="grow"></span>' +
    (list.length > 1
      ? '<div class="pager"><button class="iconbtn" type="button" data-act="prev" aria-label="Previous issue"' +
        (app.cur === 0 ? ' disabled' : '') +
        '>' +
        icon('chevron-left') +
        '</button><span>' +
        (app.cur + 1) +
        ' of ' +
        list.length +
        '</span><button class="iconbtn" type="button" data-act="next" aria-label="Next issue"' +
        (app.cur === list.length - 1 ? ' disabled' : '') +
        '>' +
        icon('chevron-right') +
        '</button></div>'
      : '') +
    '<button class="b ghost" type="button" data-act="dismiss">Dismiss <span class="kbd">Esc</span></button></div>' +
    '<div class="m-body' +
    (list.length > 1 ? '' : ' solo') +
    '">' +
    (list.length > 1 ? '<nav class="rail" aria-label="Issues">' + railHTML(app, list) + '</nav>' : '') +
    '<div class="detail">' +
    detail +
    '</div></div><div class="m-foot">' +
    footer(app, i) +
    '</div></div>';
  if (same) {
    const d = box.querySelector('.detail');
    if (d) d.scrollTop = scroll;
  }
  const term = box.querySelector('.term') as HTMLElement | null;
  if (term) term.scrollTop = term.scrollHeight;
}

/** A code frame from a script the page loaded, for stacks without a source map. */
async function servedFrame(file: string, line: number): Promise<CodeFrame | null> {
  try {
    const res = await rawFetch('/' + file.replace(/^\/+/, ''), { cache: 'force-cache' });
    if (!res.ok) return null;
    const all = (await res.text()).split('\n');
    const start = Math.max(1, line - 3);
    const lines = all.slice(start - 1, line + 3).map((l) => (l.length > 500 ? l.slice(0, 500) + '...' : l));
    // The dev hydration code registers each client scope right after its
    // body, so the next registration names the component that threw.
    let scope: string | undefined;
    for (const l of all.slice(line - 1, line + 300)) {
      const m = /__krate_dev_reg\("([^"]+)"/.exec(l);
      if (m) {
        scope = m[1];
        break;
      }
    }
    return lines.length ? { file, lang: 'js', start, lines, scope } : null;
  } catch {
    return null;
  }
}

/** Loads the code frame and fix preview of an issue, then re-renders. */
export function prepareIssue(app: KrateDev, i: Issue | undefined): void {
  if (!i) return;
  const rerender = () => {
    const cur = app.visibleIssues()[app.cur];
    if (app.modalOpen && cur && cur.id === i.id) renderModal(app);
  };
  const fk = frameKey(i);
  if (i.served && i.file && i.line && !app.frames.has(fk) && !app.busy.has('src:' + fk)) {
    app.busy.add('src:' + fk);
    void servedFrame(i.file, i.line).then((f) => {
      app.busy.delete('src:' + fk);
      app.frames.set(fk, f);
      rerender();
    });
  }
  if (canOpen(i) && !app.frames.has(fk) && !app.busy.has('src:' + fk)) {
    app.busy.add('src:' + fk);
    void app.getJSON<CodeFrame>('/source', { file: i.file, line: i.line || 1, context: 3 }).then((f) => {
      app.busy.delete('src:' + fk);
      app.frames.set(fk, f && Array.isArray(f.lines) ? f : null);
      rerender();
    });
  }
  if (i.kind === 'server' && !app.tails.has(i.id) && !app.busy.has('tail:' + i.id)) {
    app.busy.add('tail:' + i.id);
    void app.getJSON<{ lines: LogLine[] }>('/logs').then((r) => {
      app.busy.delete('tail:' + i.id);
      const lines = r && Array.isArray(r.lines) ? r.lines.filter((l) => l.t <= i.time + 1000).slice(-40) : [];
      app.tails.set(i.id, lines);
      rerender();
    });
  }
  if (i.kind === 'build' && i.code && !app.codes.has(i.code) && !app.busy.has('code:' + i.code)) {
    const code = i.code;
    app.busy.add('code:' + code);
    void app.getJSON<{ title?: string; explanation?: string }>('/code', { code }).then((r) => {
      app.busy.delete('code:' + code);
      app.codes.set(code, r && typeof r.title === 'string' ? { title: r.title, explanation: r.explanation || '' } : null);
      rerender();
    });
  }
  const xk = fixKey(i);
  if (i.kind === 'build' && i.code && i.file && !app.fixes.has(xk) && !app.busy.has('fx:' + xk)) {
    app.busy.add('fx:' + xk);
    void app.getJSON<{ changes: FixChange[] }>('/fix', { file: i.file, code: i.code }).then((r) => {
      app.busy.delete('fx:' + xk);
      app.fixes.set(xk, r && Array.isArray(r.changes) ? r.changes : null);
      rerender();
    });
  }
}

function go(app: KrateDev, n: number): void {
  const list = app.visibleIssues();
  if (n < 0 || n >= list.length || n === app.cur) return;
  app.cur = n;
  app.runtimeTab = 'stack';
  renderModal(app);
  prepareIssue(app, list[n]);
}

async function applyFix(app: KrateDev, all: boolean): Promise<void> {
  const i = app.visibleIssues()[app.cur];
  if (!i || i.kind !== 'build' || app.busy.has('fix')) return;
  if (!all && !fixFor(app, i)) return;
  app.busy.add('fix');
  renderModal(app);
  const { status, data } = await app.postJSON('/fix', all ? { all: true } : { file: i.file, code: i.code });
  app.busy.delete('fix');
  if (status === 200 && data && data.applied) {
    app.busy.add('fixed');
    const n = Array.isArray(data.changes) ? data.changes.length : 1;
    app.toast(all ? 'Applied ' + plural(n, 'fix', 'fixes') : 'Fix applied', all ? '' : basename(i.file || ''), 'check');
  } else if (status === 200) {
    app.toast('Nothing to fix', '', 'check');
  } else {
    app.toast('The fix could not be applied', data?.error || String(status || 'offline'), 'x');
  }
  renderModal(app);
}

async function restart(app: KrateDev): Promise<void> {
  const i = app.visibleIssues()[app.cur];
  const target = i?.server?.restart;
  if (!i || !target || app.busy.has('restart')) return;
  app.busy.add('restart');
  renderModal(app);
  const ok = await app.runAction('restart-sidecar', { target });
  app.busy.delete('restart');
  if (ok) {
    // The dev server does not announce a recovery; the restart is the fix.
    app.issues = app.issues.filter((x) => x.id !== i.id);
    app.issuesChanged(false);
  } else {
    renderModal(app);
  }
}

function copyAI(app: KrateDev): void {
  const i = app.visibleIssues()[app.cur];
  if (!i) return;
  const frame = app.frames.get(frameKey(i)) || null;
  const md = copyForAI(frame && !i.served ? { ...i, file: frame.file } : i, frame, { url: location.pathname + location.search, route: app.pageRoute().route });
  void copyText(md).then((ok) => app.toast(ok ? 'Copied for AI' : 'Could not copy', ok ? 'Markdown' : '', ok ? 'check' : 'x'));
}

export function modalClick(app: KrateDev, act: string, t: HTMLElement, _e: MouseEvent): boolean {
  const i = app.visibleIssues()[app.cur];
  switch (act) {
    case 'dismiss':
      app.closeModal();
      return true;
    case 'prev':
      go(app, app.cur - 1);
      return true;
    case 'next':
      go(app, app.cur + 1);
      return true;
    case 'goto':
      go(app, Number(t.dataset.n));
      return true;
    case 'open-issue':
      if (i) void app.openInEditor(i.file, i.line, i.col);
      return true;
    case 'copy-ai':
      copyAI(app);
      return true;
    case 'copy-log': {
      const log = i?.server?.log?.join('\n') || '';
      void copyText(log).then((ok) => app.toast(ok ? 'Log copied' : 'Could not copy', ok ? plural(i?.server?.log?.length || 0, 'line') : '', ok ? 'check' : 'x'));
      return true;
    }
    case 'fix':
      void applyFix(app, false);
      return true;
    case 'fix-all':
      void applyFix(app, true);
      return true;
    case 'restart':
      void restart(app);
      return true;
    case 'ignore':
      if (i) app.ignore(i.id);
      return true;
    case 'reload':
      location.reload();
      return true;
    case 'rt-tab':
      app.runtimeTab = t.dataset.v === 'console' ? 'console' : t.dataset.v === 'requests' ? 'requests' : 'stack';
      renderModal(app);
      return true;
    case 'code-pop':
      app.codePop = i && i.code && app.codePop !== i.code ? i.code : '';
      renderModal(app);
      return true;
    case 'last-good':
      app.showLastGood();
      return true;
    case 'unfold': {
      const k = t.dataset.k || '';
      if (app.unfold.has(k)) app.unfold.delete(k);
      else app.unfold.add(k);
      renderModal(app);
      return true;
    }
  }
  return false;
}

export function modalKey(app: KrateDev, e: KeyboardEvent, typing: boolean): boolean {
  if (e.key === 'Escape') {
    e.preventDefault();
    if (app.codePop) {
      app.codePop = '';
      renderModal(app);
    } else {
      app.closeModal();
    }
    return true;
  }
  if (typing || e.ctrlKey || e.metaKey || e.altKey) return false;
  const i = app.visibleIssues()[app.cur];
  switch (e.key) {
    case 'ArrowLeft':
      go(app, app.cur - 1);
      break;
    case 'ArrowRight':
      go(app, app.cur + 1);
      break;
    case 'o':
    case 'O':
      if (i && canOpen(i)) void app.openInEditor(i.file, i.line, i.col);
      break;
    case 'f':
    case 'F':
      void applyFix(app, false);
      break;
    case 'r':
    case 'R':
      if (i && i.kind === 'runtime') location.reload();
      else return false;
      break;
    default:
      return false;
  }
  e.preventDefault();
  return true;
}

