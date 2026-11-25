// The dock's panel: Route, Inspect, Activity, Logs, Settings, and the native
// Host and Memory tabs.

import type { KrateDev } from './app.js';
import type { BuildRecord, LogLine, RouteData } from './api.js';
import { icon, mark } from './icons.js';
import { type CompRow, TIERS, type Tier, hydrationOf, readTable, signalsOf, tierCounts } from './inspect.js';
import { type HostEvent, hostEventLine, jsHeapBytes, memoryRows, type MemoryReport, parseFrames } from './native.js';
import { rawFetch } from './requests.js';
import { kbds } from './settings.js';
import { clock, copyText, esc, fmtMs, kb, plural } from './util.js';

export type Tab = 'route' | 'inspect' | 'activity' | 'logs' | 'settings' | 'host' | 'memory';

const TAB_LABEL: Record<Tab, string> = {
  route: 'Route',
  inspect: 'Inspect',
  activity: 'Activity',
  logs: 'Logs',
  settings: 'Settings',
  host: 'Host',
  memory: 'Memory',
};

const WIDE: Tab[] = ['activity', 'logs', 'host', 'memory'];

const timers = new WeakMap<KrateDev, number[]>();

export function visibleTabs(app: KrateDev): Tab[] {
  const tabs: Tab[] = ['route'];
  if (readTable()) tabs.push('inspect');
  if (app.activity === 'ok' || (app.activity === 'unknown' && app.cfg.api)) tabs.push('activity');
  tabs.push('logs', 'settings');
  if (app.cfg.events) tabs.push('host');
  if (app.cfg.memory) tabs.push('memory');
  return tabs;
}

function logBadge(app: KrateDev): number {
  return mergedLogs(app).filter((l) => l.lvl === 'error' && l.t > app.logsSeenAt).length;
}

/** Renders the panel head, tabs and body; `headOnly` refreshes just the tab badges. */
export function renderPanel(app: KrateDev, headOnly = false): void {
  const p = app.panel;
  if (!p) return;
  const tabs = visibleTabs(app);
  if (!tabs.includes(app.tab)) app.tab = 'route';
  const conn =
    app.conn === 'down'
      ? '<span class="conn off">Reconnecting</span>'
      : app.building
        ? '<span class="conn busy">Rebuilding</span>'
        : '<span class="conn">Connected</span>';
  const ver = app.info?.version || app.cfg.version;
  const tabHTML = tabs
    .map((t) => {
      const n = t === 'logs' && app.tab !== 'logs' ? logBadge(app) : 0;
      return (
        '<button class="tab" type="button" role="tab" data-act="tab" data-tab="' +
        t +
        '" aria-selected="' +
        (app.tab === t) +
        '">' +
        TAB_LABEL[t] +
        (n ? ' <span class="n">' + n + '</span>' : '') +
        '</button>'
      );
    })
    .join('');
  if (headOnly && p.querySelector('.tabs')) {
    p.querySelector('.tabs')!.innerHTML = tabHTML;
    const c = p.querySelector('.conn');
    if (c) c.outerHTML = conn;
    return;
  }
  p.className = 'panel' + (WIDE.includes(app.tab) ? ' wide' : '');
  p.innerHTML =
    '<div class="p-head">' +
    mark() +
    '<span class="t">Krate</span><span class="ver">dev' +
    (ver && ver !== 'dev' ? ' \u00B7 ' + esc(ver) : '') +
    '</span><span class="grow"></span>' +
    conn +
    '<button class="iconbtn" type="button" data-act="close-panel" aria-label="Close">' +
    icon('x') +
    '</button></div><div class="tabs" role="tablist">' +
    tabHTML +
    '</div><div class="p-body" role="tabpanel"></div>';
  renderPanelBody(app);
}

export function renderPanelBody(app: KrateDev): void {
  const body = app.panel?.querySelector('.p-body') as HTMLElement | null;
  if (!body) return;
  body.classList.remove('logs');
  switch (app.tab) {
    case 'route':
      body.innerHTML = routeTab(app);
      break;
    case 'inspect':
      body.innerHTML = inspectTab(app);
      break;
    case 'activity':
      body.innerHTML = activityTab(app);
      break;
    case 'logs':
      body.classList.add('logs');
      body.innerHTML = logsTab(app);
      refreshLogs(app);
      break;
    case 'settings':
      body.innerHTML = settingsTab(app);
      break;
    case 'host':
      body.innerHTML = '<div class="hostrows" aria-live="polite">' + hostRowsHTML(app) + '</div>';
      break;
    case 'memory':
      body.innerHTML = '<dl class="kv memrows"><dt>Loading</dt><dd></dd></dl>';
      break;
  }
}

// route

const MODE_TEXT: Record<string, string> = {
  ssg: 'Prebuilt at build time',
  ssr: 'Rendered per request',
  streaming: 'Streamed per request',
  isr: 'Prebuilt, revalidated in the background',
};

function shortPage(f: string): string {
  return f.replace(/^src\/pages\//, '').replace(/^src\//, '');
}

function hasClientAncestor(rows: CompRow[], r: CompRow): boolean {
  let p = r.parent >= 0 ? rows[r.parent] : undefined;
  const seen = new Set<number>();
  while (p && !seen.has(p.n)) {
    seen.add(p.n);
    if (p.tier === 'client' || p.tier === 'island') return true;
    p = p.parent >= 0 ? rows[p.parent] : undefined;
  }
  return false;
}

/** The islands of the page: top-level client instances, external islands and runtime regions. */
export function pageIslands(rows: CompRow[]): CompRow[] {
  return rows.filter((r) => (r.tier === 'client' || r.tier === 'island' || r.tier === 'runtime') && !hasClientAncestor(rows, r));
}

export interface HeadCheck {
  name: string;
  ok: boolean;
  missing: string;
}

export function headChecks(doc: Document = document): HeadCheck[] {
  const has = (sel: string, attr = 'content') => {
    const el = doc.querySelector(sel);
    return !!el && (el.getAttribute(attr) || '').trim() !== '';
  };
  return [
    { name: 'title', ok: (doc.title || '').trim() !== '', missing: 'no title' },
    { name: 'description', ok: has('meta[name="description"]'), missing: 'no description' },
    { name: 'og:image', ok: has('meta[property="og:image"]'), missing: 'no og:image' },
    { name: 'canonical', ok: has('link[rel="canonical"]', 'href'), missing: 'no canonical' },
    { name: 'lang', ok: (doc.documentElement.getAttribute('lang') || '').trim() !== '', missing: 'no lang on <html>' },
  ];
}

const BUDGET_ROWS: [keyof NonNullable<RouteData['budget']>, string][] = [
  ['js', 'JS'],
  ['hydration', 'Hydration'],
  ['islandJS', 'Island JS'],
  ['route', 'Route total'],
];

/**
 * checks.budget against the page measured the same way the checks measure
 * it: raw bytes, not gzip. Dev builds are not minified, so production is
 * smaller than what this shows.
 */
export function budgetHTML(r: RouteData | null | undefined): string {
  const b = r?.budget;
  const u = r?.budgetUsage;
  if (!b || !u) return '';
  let rows = '';
  for (const [k, label] of BUDGET_ROWS) {
    const limit = b[k];
    if (!limit) continue;
    const used = u[k] || 0;
    rows +=
      '<div class="bgt' +
      (used > limit ? ' over' : '') +
      '"><span class="k">' +
      esc(label) +
      '</span><span class="bar"><i style="width:' +
      pct(Math.min(used, limit), Math.max(limit, 1)) +
      '"></i></span><span class="v mono">' +
      kb(used) +
      ' of ' +
      kb(limit) +
      ' KB</span></div>';
  }
  if (!rows) return '';
  return (
    '<div class="bgts">' +
    rows +
    '<div class="bgt-note">checks.budget counts raw bytes. This dev build is not minified, so the production numbers are smaller; run krate build or krate check for them.</div></div>'
  );
}

function routeTab(app: KrateDev): string {
  const r = app.route && app.routeFor === location.pathname ? app.route : null;
  const page = app.pageRoute();
  const source = r?.source || page.source;
  const mode = (r?.mode || page.mode || '').toLowerCase();
  let kv = '<dt>Pattern</dt><dd><span class="mono">' + esc(r?.pattern || page.route) + '</span></dd>';
  if (source) {
    kv +=
      '<dt>File</dt><dd><button class="mono link" type="button" data-act="open" data-file="' +
      esc(source) +
      '" title="Open in editor">' +
      esc(source) +
      '</button></dd>';
  }
  const params = r?.params ? Object.entries(r.params) : [];
  if (params.length) {
    kv +=
      '<dt>Params</dt><dd>' +
      params.map(([k, v]) => '<span class="chip"><span class="k">' + esc(k) + '</span> ' + esc(v) + '</span>').join('') +
      '</dd>';
  }
  if (mode) {
    let text = MODE_TEXT[mode] || '';
    if (mode === 'isr' && r?.revalidate) text = 'Prebuilt, revalidates every ' + r.revalidate + 's';
    if (r?.dynamic && r.staticPaths && (mode === 'ssg' || mode === 'isr')) text = '1 of ' + r.staticPaths + ' paths from generateStaticParams';
    kv += '<dt>Rendering</dt><dd><span class="mode ' + esc(mode) + '">' + esc(mode.toUpperCase()) + '</span><span class="dim">' + esc(text) + '</span></dd>';
  }
  if (r?.layouts && r.layouts.length) {
    kv += '<dt>Layouts</dt><dd><span class="mono dim" title="' + esc(r.layouts.join('\n')) + '">' + esc(r.layouts.map(shortPage).join(' \u203A ')) + '</span></dd>';
  }
  let html = '<div class="sect"><dl class="kv">' + kv + '</dl></div>';

  const rows = readTable() || [];
  const islands = pageIslands(rows);
  let hydrate = 0;
  let hydrated = false;
  for (const row of islands) {
    const ms = hydrationOf(row);
    if (ms !== undefined) {
      hydrate += ms;
      hydrated = true;
    }
  }
  const s = r?.sizes;
  if (s) {
    const js = s.gzipJs || 0;
    const total = Math.max(js, 1);
    const rt = s.gzipRuntime || 0;
    const isl = s.gzipIslands || 0;
    const other = Math.max(0, js - rt - isl);
    html +=
      '<div class="sect"><div class="lbl">Shipped</div><div class="stats">' +
      stat('HTML', kb(s.html), 'KB') +
      stat('JS gz', kb(js), 'KB') +
      stat('CSS', kb(s.css), 'KB') +
      stat('Hydrate', hydrated ? (hydrate < 10 ? hydrate.toFixed(1) : String(Math.round(hydrate))) : '0', 'ms') +
      '</div><div style="margin-top:12px"><div class="bar">' +
      '<i style="width:' +
      pct(rt, total) +
      ';background:var(--kd-fg3)"></i><i style="width:' +
      pct(isl, total) +
      ';background:var(--kd-accent)"></i><i style="width:' +
      pct(other, total) +
      ';background:var(--kd-blue)"></i></div><div class="budget"><span><span style="color:var(--kd-fg3)">runtime ' +
      kb(rt) +
      '</span> \u00B7 <span style="color:var(--kd-accent-fg)">islands ' +
      kb(isl) +
      '</span>' +
      (other > 0 ? ' \u00B7 <span style="color:var(--kd-blue)">page ' + kb(other) + '</span>' : '') +
      '</span><span>' +
      kb(js) +
      ' KB gzipped</span></div></div>' +
      budgetHTML(r) +
      '</div>';
  }

  html += '<div class="sect"><div class="lbl">Islands on this page</div>';
  if (islands.length) {
    html += '<div class="rows">';
    for (const row of islands) {
      let sub = '';
      if (row.tier === 'client') {
        const n = signalsOf(row.key).length;
        sub = n ? plural(n, 'signal') : 'no signals';
      } else if (row.tier === 'island') sub = 'framework island';
      else sub = 'rendered per request';
      const gz = row.island ? r?.islands?.find((i) => i.id === row.island)?.gzip : undefined;
      const ms = hydrationOf(row);
      const right: string[] = [];
      if (gz !== undefined) right.push(kb(gz) + ' KB');
      if (row.tier === 'runtime') right.push('per request');
      else if (ms !== undefined) right.push(fmtMs(ms));
      html +=
        '<button class="row" type="button" data-act="open" data-file="' +
        esc(row.file || '') +
        '" data-line="' +
        esc(row.line || '') +
        '"><span class="name mono">' +
        esc(row.name) +
        '</span><span class="tp ' +
        row.tier +
        '">' +
        row.tier +
        '</span><span class="sub">' +
        esc(sub) +
        '</span><span class="right mono">' +
        right.map(esc).join('<span class="dim">\u00B7</span>') +
        '</span></button>';
    }
    html += '</div>';
  } else {
    html += '<div class="empty">' + (rows.length ? 'None. This page ships no component JavaScript.' : 'No component data for this page.') + '</div>';
  }
  html += '</div>';

  html +=
    '<div class="sect"><div class="lbl">Head</div><div class="actions">' +
    headChecks()
      .map((c) =>
        c.ok
          ? '<span class="chip ok">' + icon('check') + esc(c.name) + '</span>'
          : '<span class="chip warn">' + icon('circle-exclamation-point') + esc(c.missing) + '</span>',
      )
      .join('') +
    '</div></div>';

  html += '<div class="sect"><div class="actions">';
  if (source) html += '<button class="b" type="button" data-act="open" data-file="' + esc(source) + '">' + icon('square-arrow-up-right-exit') + 'Open page source</button>';
  if (app.cfg.api && r) html += '<button class="b" type="button" data-act="view-html">View built HTML</button>';
  if (app.cfg.api && r) html += '<button class="b ghost" type="button" data-act="rebuild-page">Rebuild page</button>';
  html += '<button class="b ghost" type="button" data-act="copy-url">Copy URL</button></div></div>';
  return html;
}

function stat(k: string, v: string, unit: string): string {
  return '<div class="stat"><div class="k">' + esc(k) + '</div><div class="v">' + esc(v) + '<small> ' + esc(unit) + '</small></div></div>';
}

function pct(n: number, total: number): string {
  return Math.max(0, Math.min(100, (n / total) * 100)).toFixed(1) + '%';
}

// inspect

const TIER_TEXT: Record<Tier, [string, string]> = {
  static: ['Static', 'Rendered at build time, ships no JavaScript'],
  server: ['Server', 'Server component, rendered at build time'],
  client: ['Client', 'Hydrated in the browser, with signals'],
  runtime: ['Runtime', 'Rendered per request by the server'],
  island: ['Island', 'Preact or React island'],
};

function inspectTab(app: KrateDev): string {
  const rows = readTable() || [];
  const counts = tierCounts(rows);
  let html =
    '<div class="sect"><div class="setrow"><div><div class="t">Component inspector</div><div class="d">Outline every component by tier. Hover for details.</div></div>' +
    '<button class="tog" type="button" role="switch" data-act="inspect" aria-label="Component inspector" aria-checked="' +
    app.inspector.on +
    '"></button></div></div><div class="sect"><div class="lbl">On this page</div><div class="rows tiers">';
  for (const t of TIERS) {
    if (t === 'island' && !counts.island) continue;
    html +=
      '<div class="row"><span class="tp ' +
      t +
      '">' +
      TIER_TEXT[t][0] +
      '</span><span class="sub">' +
      esc(TIER_TEXT[t][1]) +
      '</span><span class="right mono">' +
      counts[t] +
      '</span></div>';
  }
  html += '</div>';
  const isl = app.route?.sizes?.gzipIslands;
  if (isl) html += '<div class="note">' + kb(isl) + ' KB of island JavaScript, gzipped.</div>';
  html +=
    '</div><div class="sect"><div class="keys"><span>Toggle the inspector</span><span>' +
    kbds(['Alt', 'I']) +
    '</span><span>Open the source</span><span>Click</span><span>Copy the path</span><span>Shift-click</span><span>Stop inspecting</span><span><span class="kbd">Esc</span></span></div></div>';
  return html;
}

// activity

const PHASE_COLORS = ['var(--kd-fg3)', 'var(--kd-blue)', 'var(--kd-accent)', 'var(--kd-violet)', 'var(--kd-ok)', 'var(--kd-static-fg)', 'var(--kd-pink)', 'var(--kd-warn)'];

const expandedBuilds = new WeakMap<KrateDev, Set<number>>();

function median(xs: number[]): number {
  if (!xs.length) return 0;
  const s = [...xs].sort((a, b) => a - b);
  const m = s.length >> 1;
  return s.length % 2 ? s[m] : (s[m - 1] + s[m]) / 2;
}

function msStat(ms: number): [string, string] {
  if (ms >= 1000) return [(ms / 1000).toFixed(1), 's'];
  return [String(Math.round(ms)), 'ms'];
}

function shortFile(f: string): string {
  return f.replace(/^src\//, '');
}

export function buildHeadline(b: BuildRecord): string {
  const files = b.files || [];
  const code = (f: string) => '<code>' + esc(shortFile(f)) + '</code>';
  if (!b.ok) return 'Build failed, ' + plural(Math.max(1, b.diagnostics), 'issue');
  if (files.length === 1) return code(files[0]) + ' changed';
  if (files.length > 1) return code(files[0]) + ' and ' + (files.length - 1) + ' more changed';
  if (b.kind === 'initial') return 'Initial build';
  if (b.kind === 'config-restart') return 'Config changed';
  return 'Full rebuild';
}

export function buildDetail(b: BuildRecord, later: BuildRecord[], route: string): string {
  if (!b.ok) {
    const fix = later.find((x) => x.ok);
    if (fix) {
      const secs = Math.max(1, Math.round((fix.start - b.start) / 1000));
      const f = fix.files && fix.files[0];
      return 'Fixed ' + secs + 's later' + (f ? ' in <span class="mono">' + esc(shortFile(f)) + '</span>' : '');
    }
    return b.files && b.files[0] ? 'In <span class="mono">' + esc(shortFile(b.files[0])) + '</span>' : 'Not fixed yet';
  }
  switch (b.kind) {
    case 'css-only':
      return 'Styles swapped in place, no reload';
    case 'config-restart':
      return 'Dev server restarted, sidecars respawned';
    case 'public-only':
      return 'Public files copied';
  }
  const here = (b.routes || []).some((r) => r === route);
  const pages = b.pages ? plural(b.pages, 'page') + ' rebuilt' : 'Rebuilt';
  return pages + (b.kind === 'full' || b.kind === 'initial' ? '' : here ? ', this tab reloaded' : '');
}

/** The icon of a build record and its color. */
function buildIcon(b: BuildRecord): [string, string] {
  if (!b.ok) return ['circle-x', 'var(--kd-err)'];
  if (b.kind === 'css-only') return ['palette', 'var(--kd-blue)'];
  if (b.kind === 'config-restart') return ['rotate-cw', 'var(--kd-warn)'];
  if (b.kind === 'full' || b.kind === 'initial' || b.pages > 1) return ['zap', 'var(--kd-accent)'];
  return ['check', 'var(--kd-ok)'];
}

function activityTab(app: KrateDev): string {
  const builds = app.builds;
  if (!builds.length) return '<div class="empty">No builds recorded yet.</div>';
  const ok = builds.filter((b) => b.ok);
  const durs = ok.map((b) => b.durationMs);
  const slow = durs.length ? Math.max(...durs) : 0;
  const [mv, mu] = msStat(median(durs));
  const [sv, su] = msStat(slow);
  const last = builds.slice(-12);
  const top = Math.max(1, ...last.map((b) => b.durationMs));
  const topId = last.reduce((a, b) => (b.durationMs > a.durationMs ? b : a), last[0]).id;
  let html =
    '<div class="sect actgrid"><div class="stats" style="grid-template-columns:repeat(3,1fr)">' +
    stat('Rebuilds', String(builds.length), '') +
    stat('Median', mv, mu) +
    stat('Slowest', sv, su) +
    '</div><div><div class="spark" aria-hidden="true">' +
    last
      .map(
        (b) =>
          '<i class="' +
          (!b.ok ? 'bad' : b.id === topId ? 'hi' : '') +
          '" style="height:' +
          Math.max(6, Math.round((b.durationMs / top) * 100)) +
          '%" title="' +
          esc(fmtMs(b.durationMs)) +
          '"></i>',
      )
      .join('') +
    '</div><div style="font-size:11px;color:var(--kd-fg3);margin-top:4px">last ' +
    plural(last.length, 'rebuild') +
    '</div></div></div><div class="sect">';
  const shown = builds.slice(-30).reverse();
  const slowest = shown.reduce((a, b) => (b.durationMs > a.durationMs ? b : a), shown[0]);
  const exp = expandedBuilds.get(app) || new Set<number>();
  const route = app.pageRoute().route;
  shown.forEach((b) => {
    const idx = builds.indexOf(b);
    const later = builds.slice(idx + 1);
    const showPhases = (b.phases?.length || 0) > 1 && (exp.has(b.id) || (b.id === slowest.id && !exp.has(-b.id - 1)));
    const [ic, color] = buildIcon(b);
    html +=
      '<div class="ev' +
      ((b.phases?.length || 0) > 1 ? ' click' : '') +
      '" data-act="phases" data-id="' +
      b.id +
      '"><span class="time">' +
      clock(b.start) +
      '</span><span class="ic" style="color:' +
      color +
      '">' +
      icon(ic) +
      '</span><div class="what"><div class="h">' +
      buildHeadline(b) +
      (!b.ok && app.issues.some((i) => i.kind === 'build') && !later.length
        ? ' <button class="link" type="button" data-act="issues" style="font-size:12px;margin-left:4px">view</button>'
        : '') +
      '</div><div class="s">' +
      buildDetail(b, later, route) +
      '</div>' +
      (showPhases ? phasesHTML(b) : '') +
      '</div><span class="dur">' +
      esc(fmtMs(b.durationMs)) +
      '</span></div>';
  });
  return html + '</div>';
}

function phasesHTML(b: BuildRecord): string {
  const ph = (b.phases || []).filter((p) => p.ms > 0);
  const total = ph.reduce((a, p) => a + p.ms, 0) || 1;
  return (
    '<div class="phases"><div class="bar">' +
    ph.map((p, i) => '<i style="width:' + pct(p.ms, total) + ';background:' + PHASE_COLORS[i % PHASE_COLORS.length] + '" title="' + esc(p.name) + '"></i>').join('') +
    '</div><div class="plegend">' +
    ph.map((p, i) => '<span><i style="background:' + PHASE_COLORS[i % PHASE_COLORS.length] + '"></i>' + esc(p.name) + ' ' + esc(fmtMs(p.ms)) + '</span>').join('') +
    '</div></div>'
  );
}

// logs

const SERVER_SRC = new Set(['krate', 'ssr', 'go', 'server', 'middleware']);

export function mergedLogs(app: KrateDev): LogLine[] {
  const local = app.localLogs;
  const server = app.serverLogs.filter(
    (l) => !(l.src === 'browser' && local.some((x) => l.text.includes(x.text.replace(/^console\.(error|warn) /, '').slice(0, 80)))),
  );
  return [...server, ...local].filter((l) => l.t > app.logsClearedAt).sort((a, b) => a.t - b.t || a.seq - b.seq);
}

export function filterLogs(lines: LogLine[], filter: KrateDev['logFilter'], query: string, errorsOnly: boolean): LogLine[] {
  const q = query.trim().toLowerCase();
  return lines.filter((l) => {
    if (errorsOnly && l.lvl !== 'error') return false;
    if (filter === 'server' && !SERVER_SRC.has(l.src)) return false;
    if (filter === 'api' && l.src !== 'api') return false;
    if (filter === 'browser' && l.src !== 'browser') return false;
    return !q || l.text.toLowerCase().includes(q) || l.src.includes(q);
  });
}

function logsTab(app: KrateDev): string {
  const seg = (['all', 'server', 'api', 'browser'] as const)
    .map(
      (f) =>
        '<button type="button" data-act="logf" data-v="' +
        f +
        '" aria-pressed="' +
        (app.logFilter === f) +
        '">' +
        (f === 'all' ? 'All' : f === 'api' ? 'API' : f[0].toUpperCase() + f.slice(1)) +
        '</button>',
    )
    .join('');
  return (
    '<div class="toolbar2"><div class="segc" role="group" aria-label="Source">' +
    seg +
    '</div><label class="search">' +
    icon('search') +
    '<input type="text" data-input="logq" placeholder="Filter" aria-label="Filter logs" value="' +
    esc(app.logQuery) +
    '"></label><button class="b ghost" type="button" data-act="errors-only" aria-pressed="' +
    app.errorsOnly +
    '">Errors only</button><button class="iconbtn" type="button" data-act="clear-logs" title="Clear" aria-label="Clear logs">' +
    icon('trash') +
    '</button></div><div class="log" role="log"></div>' +
    (app.logsMissing || !app.cfg.api ? '<div class="note">Showing this tab only; this dev server does not share its log.</div>' : '')
  );
}

export function refreshLogs(app: KrateDev): void {
  const box = app.panel?.querySelector('.log') as HTMLElement | null;
  if (!box) {
    renderPanel(app, true);
    return;
  }
  const lines = filterLogs(mergedLogs(app), app.logFilter, app.logQuery, app.errorsOnly).slice(-500);
  const pinned = box.scrollTop + box.clientHeight >= box.scrollHeight - 8;
  box.innerHTML = lines.length
    ? lines
        .map((l) => {
          const key = (l.local ? 'l' : 's') + l.seq;
          const parts = l.text.replace(/^\s+/, '').split('\n');
          const open = app.logOpen.has(key);
          const head = open || parts.length <= 2 ? parts.join('\n') : parts.slice(0, 2).join('\n');
          const more =
            parts.length > 2
              ? '<button class="more" type="button" data-act="logmore" data-k="' +
                key +
                '">' +
                (open ? 'Show less' : '+ ' + plural(parts.length - 2, 'more line')) +
                '</button>'
              : '';
          return (
            '<div class="lg' +
            (l.lvl === 'error' ? ' e' : l.lvl === 'warn' ? ' w' : '') +
            '"><span class="ts">' +
            clock(l.t) +
            '</span><span class="src s-' +
            esc(l.src) +
            '">' +
            esc(l.src) +
            '</span><span class="m">' +
            esc(head) +
            more +
            '</span></div>'
          );
        })
        .join('')
    : '<div class="lg"><span></span><span></span><span class="m" style="color:var(--kd-fg3)">No log lines' +
      (app.logQuery || app.errorsOnly || app.logFilter !== 'all' ? ' match the filters' : ' yet') +
      '.</span></div>';
  if (pinned) box.scrollTop = box.scrollHeight;
  app.logsSeenAt = Date.now();
}

async function pollLogs(app: KrateDev): Promise<void> {
  if (!app.cfg.api || app.logsMissing) return;
  const res = await app.getJSON<{ seq: number; lines: LogLine[] }>('/logs', { since: app.logSeq || undefined });
  if (!res || !Array.isArray(res.lines)) {
    app.logsMissing = true;
    if (app.tab === 'logs') renderPanelBody(app);
    return;
  }
  app.logSeq = res.seq || app.logSeq;
  if (res.lines.length) {
    app.serverLogs.push(...res.lines);
    if (app.serverLogs.length > 500) app.serverLogs.splice(0, app.serverLogs.length - 500);
    if (app.panelOpen && app.tab === 'logs') refreshLogs(app);
  }
}

// settings

function settingsTab(app: KrateDev): string {
  const s = app.settings;
  const corner = (['tl', 'tr', 'bl', 'br'] as const)
    .map(
      (c) =>
        '<button class="' +
        c +
        '" type="button" data-act="corner" data-v="' +
        c +
        '" aria-pressed="' +
        (s.corner === c) +
        '" aria-label="' +
        { tl: 'Top left', tr: 'Top right', bl: 'Bottom left', br: 'Bottom right' }[c] +
        '"></button>',
    )
    .join('');
  const theme = (['system', 'dark', 'light'] as const)
    .map((t) => '<button type="button" data-act="theme" data-v="' + t + '" aria-pressed="' + (s.theme === t) + '">' + t[0].toUpperCase() + t.slice(1) + '</button>')
    .join('');
  let html =
    '<div class="sect" style="display:flex;gap:16px;align-items:center"><div class="corner" role="group" aria-label="Dock position">' +
    corner +
    '</div><div><div style="font-weight:500">Dock position</div><div style="font-size:12px;color:var(--kd-fg3);margin-top:2px">Or drag the dock to any corner.</div>' +
    '<div class="segc" role="group" aria-label="Theme" style="margin-top:10px">' +
    theme +
    '</div></div></div><div class="sect">';
  const ed = app.info?.editor;
  if (ed) {
    const avail = ed.available || [];
    const cur = avail.find((e) => e.id === s.editor);
    const configured = ed.configured || 'code';
    const label = cur ? cur.label : 'Default';
    const cmd = cur ? cur.id : configured;
    html +=
      '<div class="setrow"><div><div class="t">Editor</div><div class="d">Used by Open in editor and inspector clicks</div></div><label class="select">' +
      esc(label) +
      ' <span class="mono cmd">' +
      esc(cmd) +
      '</span>' +
      icon('chevron-down') +
      '<select data-input="editor" aria-label="Editor"><option value="">Default (' +
      esc(configured) +
      ')</option>' +
      avail.map((e) => '<option value="' + esc(e.id) + '"' + (e.id === s.editor ? ' selected' : '') + '>' + esc(e.label) + '</option>').join('') +
      '</select></label></div>';
  }
  const tog = (k: 'openOnRuntime' | 'consoleIssues' | 'showWarnings' | 'collapsed', t: string, d: string) =>
    '<div class="setrow"><div><div class="t">' +
    esc(t) +
    '</div>' +
    (d ? '<div class="d">' + esc(d) + '</div>' : '') +
    '</div><button class="tog" type="button" role="switch" data-act="setting" data-k="' +
    k +
    '" aria-label="' +
    esc(t) +
    '" aria-checked="' +
    s[k] +
    '"></button></div>';
  html +=
    tog('openOnRuntime', 'Open overlay on runtime errors', 'Off: only the dock count changes') +
    tog('consoleIssues', 'Treat console.error as an issue', '') +
    tog('showWarnings', 'Show build warnings', 'Listed with the issues; they never open the overlay') +
    tog('collapsed', 'Collapse dock to the mark', 'Expands on hover') +
    '</div><div class="sect"><div class="lbl">Shortcuts</div><div class="keys">' +
    '<span>Command menu</span><span>' +
    kbds(['Alt', 'K']) +
    '</span><span>Component inspector</span><span>' +
    kbds(['Alt', 'I']) +
    '</span><span>Show issues</span><span>' +
    kbds(['Alt', 'E']) +
    '</span><span>Hide everything</span><span>' +
    kbds(['Alt', '.']) +
    '</span></div></div>';
  return html;
}

// native host and memory

function hostRowsHTML(app: KrateDev): string {
  return app.hostRows.length ? app.hostRows.join('') : '<div class="hostrow sig">Waiting for host calls</div>';
}

async function pollHost(app: KrateDev): Promise<void> {
  try {
    const res = await rawFetch(app.cfg.events + '?since=' + app.hostSeq, { cache: 'no-store' });
    if (!res.ok) return;
    const parsed = parseFrames(new Uint8Array(await res.arrayBuffer()));
    for (const v of parsed.values) {
      const ev = (v as { ev?: HostEvent } | null)?.ev;
      if (!ev) continue;
      app.hostSeq = Math.max(app.hostSeq, ev.seq);
      app.hostRows.push('<div class="hostrow' + (ev.code ? ' err' : ev.kind === 'signal' ? ' sig' : '') + '">' + esc(hostEventLine(ev)) + '</div>');
    }
    if (app.hostRows.length > 200) app.hostRows.splice(0, app.hostRows.length - 200);
    const rows = app.panel?.querySelector('.hostrows') as HTMLElement | null;
    if (rows && app.tab === 'host') {
      const pinned = rows.scrollTop + rows.clientHeight >= rows.scrollHeight - 8;
      rows.innerHTML = hostRowsHTML(app);
      if (pinned) rows.scrollTop = rows.scrollHeight;
    }
  } catch {
    /* the tab is best-effort */
  }
}

async function pollMemory(app: KrateDev): Promise<void> {
  let rep: MemoryReport | null = null;
  try {
    const res = await rawFetch(app.cfg.memory, { cache: 'no-store' });
    if (res.ok) rep = (await res.json()) as MemoryReport;
  } catch {
    /* the tab is best-effort */
  }
  const box = app.panel?.querySelector('.memrows') as HTMLElement | null;
  if (!box || app.tab !== 'memory') return;
  box.innerHTML = memoryRows(rep, document.getElementsByTagName('*').length, jsHeapBytes())
    .map(([k, v]) => '<dt>' + esc(k) + '</dt><dd><span class="mono">' + esc(v) + '</span></dd>')
    .join('');
}

export function stopPanelTimers(app: KrateDev): void {
  for (const t of timers.get(app) || []) clearInterval(t);
  timers.delete(app);
}

/** Polls what the open tab shows: logs every second, host calls every second, memory every 2 seconds. */
export function startPanelTimers(app: KrateDev): void {
  stopPanelTimers(app);
  if (!app.panelOpen) return;
  const list: number[] = [];
  const every = (fn: () => void, ms: number) => {
    fn();
    list.push(setInterval(fn, ms) as unknown as number);
  };
  if (app.tab === 'logs' && app.cfg.api && !app.logsMissing) every(() => void pollLogs(app), 1000);
  if (app.tab === 'host' && app.cfg.events) every(() => void pollHost(app), 1000);
  if (app.tab === 'memory' && app.cfg.memory) every(() => void pollMemory(app), 2000);
  timers.set(app, list);
}

// clicks

export function panelClick(app: KrateDev, act: string, t: HTMLElement, _e: MouseEvent): boolean {
  switch (act) {
    case 'close-panel':
      app.togglePanel(undefined, false);
      return true;
    case 'tab':
      app.setTab((t.dataset.tab as Tab) || 'route');
      return true;
    case 'copy-url':
      void copyText(location.href).then((ok) => app.toast(ok ? 'URL copied' : 'Could not copy', ok ? location.pathname : '', ok ? 'check' : 'x'));
      return true;
    case 'view-html':
      window.open(app.apiURL('/html', { path: location.pathname }), '_blank', 'noopener');
      return true;
    case 'rebuild-page':
      void app.runAction('rebuild-page', { route: location.pathname });
      return true;
    case 'corner':
      app.setSettings({ corner: t.dataset.v as any });
      return true;
    case 'theme':
      app.setSettings({ theme: t.dataset.v as any });
      return true;
    case 'setting': {
      const k = t.dataset.k as 'openOnRuntime' | 'consoleIssues' | 'showWarnings' | 'collapsed';
      app.setSettings({ [k]: !app.settings[k] });
      if (k === 'showWarnings') app.issuesChanged(false);
      if (k === 'consoleIssues' && !app.settings.consoleIssues) {
        app.issues = app.issues.filter((i) => i.origin !== 'console');
        app.issuesChanged(false);
      }
      return true;
    }
    case 'logf':
      app.logFilter = (t.dataset.v as KrateDev['logFilter']) || 'all';
      for (const b of Array.from(app.panel!.querySelectorAll('[data-act=logf]'))) b.setAttribute('aria-pressed', String(b === t));
      refreshLogs(app);
      return true;
    case 'errors-only':
      app.errorsOnly = !app.errorsOnly;
      t.setAttribute('aria-pressed', String(app.errorsOnly));
      refreshLogs(app);
      return true;
    case 'clear-logs':
      app.logsClearedAt = Date.now();
      app.localLogs = [];
      refreshLogs(app);
      return true;
    case 'logmore': {
      const k = t.dataset.k || '';
      if (app.logOpen.has(k)) app.logOpen.delete(k);
      else app.logOpen.add(k);
      refreshLogs(app);
      return true;
    }
    case 'phases': {
      const id = Number(t.dataset.id);
      const set = expandedBuilds.get(app) || new Set<number>();
      const b = app.builds.find((x) => x.id === id);
      if (!b || (b.phases?.length || 0) < 2) return true;
      const shownByDefault = !set.has(id) && !set.has(-id - 1) && t.querySelector('.phases');
      if (set.has(id)) set.delete(id);
      else if (shownByDefault) set.add(-id - 1);
      else if (set.has(-id - 1)) set.delete(-id - 1);
      else set.add(id);
      expandedBuilds.set(app, set);
      renderPanelBody(app);
      return true;
    }
  }
  return false;
}

