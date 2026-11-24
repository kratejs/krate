// Command menu items and their filtering.

import { esc } from './util.js';

export interface RouteInfo {
  pattern: string;
  source?: string;
  mode?: string;
  revalidate?: number;
  kind: 'page' | 'api';
  methods?: string[];
  paths?: string[];
}

export interface PaletteItem {
  id: string;
  group: 'Routes' | 'Actions';
  label: string;
  mono?: boolean;
  /** Indented under its route (a concrete path of a dynamic route). */
  sub?: boolean;
  chip?: string;
  chipClass?: string;
  right?: string;
  icon?: string;
  keys?: string[];
  /** URL to navigate to, for route items. */
  href?: string;
  /** Project file Cmd/Ctrl+Enter opens. */
  source?: string;
  action?: string;
}

export interface Match {
  score: number;
  ranges: [number, number][];
}

/**
 * Matches a query against text: a substring scores best (earlier and at a
 * segment start is better), then a subsequence. Null when it does not match.
 */
export function fuzzyMatch(query: string, text: string): Match | null {
  const q = query.trim().toLowerCase();
  if (!q) return { score: 0, ranges: [] };
  const t = text.toLowerCase();
  const at = t.indexOf(q);
  if (at !== -1) {
    const boundary = at === 0 || /[/\s\-_.[]/.test(t[at - 1]);
    return { score: 1000 - at + (boundary ? 200 : 0) - t.length / 100, ranges: [[at, at + q.length]] };
  }
  const ranges: [number, number][] = [];
  let ti = 0;
  for (const ch of q) {
    if (ch === ' ') continue;
    const idx = t.indexOf(ch, ti);
    if (idx === -1) return null;
    const last = ranges[ranges.length - 1];
    if (last && last[1] === idx) last[1] = idx + 1;
    else ranges.push([idx, idx + 1]);
    ti = idx + 1;
  }
  return { score: 100 - ranges.length * 10 - t.length / 100, ranges };
}

/** Text with the matched ranges wrapped in <mark>, escaped. */
export function markRanges(text: string, ranges: [number, number][]): string {
  let out = '';
  let pos = 0;
  for (const [a, b] of ranges) {
    out += esc(text.slice(pos, a)) + '<mark>' + esc(text.slice(a, b)) + '</mark>';
    pos = b;
  }
  return out + esc(text.slice(pos));
}

function isDynamic(pattern: string): boolean {
  return pattern.includes('[');
}

/** Command menu items for the routes /__krate/dev/routes lists. */
export function routeItems(routes: RouteInfo[], query: string): PaletteItem[] {
  const items: PaletteItem[] = [];
  const q = query.trim();
  for (const r of routes) {
    const dyn = isDynamic(r.pattern);
    const paths = r.paths || [];
    const api = r.kind === 'api';
    let right = r.source || '';
    if (api) right = r.methods && r.methods.length ? r.methods.join(' ') : r.source || '';
    else if (dyn) right = paths.length + (paths.length === 1 ? ' path' : ' paths');
    else if (r.revalidate) right = 'revalidate ' + r.revalidate + 's';
    const mode = api ? 'API' : (r.mode || 'ssg').toUpperCase();
    items.push({
      id: 'route:' + r.kind + ':' + r.pattern,
      group: 'Routes',
      label: r.pattern,
      mono: true,
      chip: mode,
      chipClass: api ? 'api' : (r.mode || 'ssg').toLowerCase(),
      right,
      href: dyn ? paths[0] : r.pattern,
      source: r.source,
    });
    if (dyn && q) {
      for (const p of paths) {
        if (p === r.pattern) continue;
        items.push({
          id: 'path:' + p,
          group: 'Routes',
          label: p,
          mono: true,
          sub: true,
          right: r.pattern,
          href: p,
          source: r.source,
        });
      }
    }
  }
  return items;
}

/**
 * Filters and orders items for the query. Routes come before actions; within a
 * group the best match comes first. An empty query keeps the given order, and
 * concrete paths only show when they match.
 */
export function filterItems(items: PaletteItem[], query: string, limit = 60): { item: PaletteItem; match: Match }[] {
  const q = query.trim();
  const out: { item: PaletteItem; match: Match; i: number }[] = [];
  items.forEach((item, i) => {
    const m = fuzzyMatch(q, item.label);
    if (!m) return;
    if (item.sub && m.ranges.length !== 1) return;
    out.push({ item, match: m, i });
  });
  const order = (g: string) => (g === 'Routes' ? 0 : 1);
  out.sort((a, b) => order(a.item.group) - order(b.item.group) || (q ? b.match.score - a.match.score : 0) || a.i - b.i);
  const routes = out.filter((o) => o.item.group === 'Routes').slice(0, limit);
  const actions = out.filter((o) => o.item.group === 'Actions');
  return [...routes, ...actions].map(({ item, match }) => ({ item, match }));
}
