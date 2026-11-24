// The command menu (Alt+K): routes and dev actions.

import type { KrateDev } from './app.js';
import { type PaletteItem, filterItems, markRanges, routeItems } from './commands.js';
import { icon } from './icons.js';
import { readTable } from './inspect.js';
import { kbd } from './settings.js';
import { esc, isMac } from './util.js';

interface PaletteState {
  query: string;
  sel: number;
  shown: PaletteItem[];
}

const state = new WeakMap<KrateDev, PaletteState>();

/** The dev actions the menu offers on this page. */
export function actionItems(app: KrateDev): PaletteItem[] {
  const items: PaletteItem[] = [];
  const add = (action: string, label: string, ic: string, keys?: string[]) =>
    items.push({ id: 'action:' + action, group: 'Actions', label, icon: ic, keys, action });
  const page = app.pageRoute();
  if (readTable()) add('inspect', 'Toggle component inspector', 'mouse-pointer-2', ['Alt', 'I']);
  if (app.visibleIssues().length) add('issues', 'Show issues', 'triangle-exclamation-point', ['Alt', 'E']);
  if (app.cfg.api) {
    add('rebuild-page', 'Rebuild this page', 'rotate-cw');
    add('rebuild-all', 'Rebuild everything', 'refresh-cw');
    add('restart-server', 'Restart dev server', 'monitor');
    add('clear-cache', 'Clear .krate cache and rebuild', 'trash');
  }
  if (page.source) add('open-source', 'Open page source', 'file-code');
  if (app.cfg.toolbar) add('settings', 'Open settings', 'settings');
  add('hide', 'Hide the overlay', 'eye-off', ['Alt', '.']);
  return items;
}

export function openPalette(app: KrateDev): void {
  if (app.pal && !app.pal.hidden) {
    closePalette(app);
    return;
  }
  if (!app.pal) {
    const wrap = document.createElement('div');
    wrap.className = 'pal-wrap';
    wrap.innerHTML =
      '<div class="pal" role="dialog" aria-modal="true" aria-label="Command menu"><div class="pal-in">' +
      icon('search') +
      '<input type="text" role="combobox" aria-expanded="true" aria-controls="kd-pal-list" aria-autocomplete="list" placeholder="Go to a route or run an action" autocomplete="off" spellcheck="false">' +
      '<span class="kbd">Esc</span></div><div class="pal-list" id="kd-pal-list" role="listbox"></div><div class="pal-f"></div></div>';
    app.root.appendChild(wrap);
    app.pal = wrap;
    const input = wrap.querySelector('input') as HTMLInputElement;
    input.addEventListener('input', () => {
      const st = state.get(app)!;
      st.query = input.value;
      st.sel = 0;
      renderList(app);
    });
    wrap.addEventListener('mousemove', (e) => {
      const el = (e.target as HTMLElement).closest('.pi') as HTMLElement | null;
      if (!el) return;
      const n = Number(el.dataset.n);
      const st = state.get(app)!;
      if (n !== st.sel) {
        st.sel = n;
        mark(app);
      }
    });
  }
  const mac = isMac();
  (app.pal.querySelector('.pal-f') as HTMLElement).innerHTML =
    '<span><span class="kbd">' +
    icon('arrow-up') +
    '</span><span class="kbd">' +
    icon('arrow-down') +
    '</span>move</span><span>' +
    kbd(['Enter'], mac) +
    'open</span><span>' +
    kbd(['Mod', 'Enter'], mac) +
    'open source file</span><span class="grow"></span><span>' +
    kbd(['Alt', 'K'], mac) +
    'toggle</span>';
  state.set(app, { query: '', sel: 0, shown: [] });
  const input = app.pal.querySelector('input') as HTMLInputElement;
  input.value = '';
  app.pal.hidden = false;
  renderList(app);
  try {
    input.focus({ preventScroll: true });
  } catch {
    /* focus is a nicety */
  }
  if (app.routes === undefined && app.cfg.api) {
    void app.loadRoutes().then(() => {
      if (app.pal && !app.pal.hidden) renderList(app);
    });
  }
}

export function closePalette(app: KrateDev): void {
  if (app.pal) app.pal.hidden = true;
}

function renderList(app: KrateDev): void {
  const st = state.get(app);
  const box = app.pal?.querySelector('.pal-list') as HTMLElement | null;
  if (!st || !box) return;
  const items = [...routeItems(app.routes || [], st.query), ...actionItems(app)];
  const res = filterItems(items, st.query);
  st.shown = res.map((r) => r.item);
  if (st.sel >= st.shown.length) st.sel = Math.max(0, st.shown.length - 1);
  if (!res.length) {
    box.innerHTML = '<div class="pal-empty">' + (app.routes === undefined && app.cfg.api ? 'Loading routes' : 'Nothing matches') + '</div>';
    return;
  }
  let html = '';
  let group = '';
  res.forEach(({ item, match }, n) => {
    if (item.group !== group) {
      if (group) html += '</div>';
      html += '<div class="pal-g" role="group" aria-label="' + item.group + '"><div class="lbl">' + item.group + '</div>';
      group = item.group;
    }
    html +=
      '<button class="pi' +
      (item.sub ? ' sub' : '') +
      '" type="button" role="option" id="kd-pi-' +
      n +
      '" data-act="pal-run" data-n="' +
      n +
      '" aria-selected="' +
      (n === st.sel) +
      '">' +
      (item.chip ? '<span class="mode ' + esc(item.chipClass || '') + '">' + esc(item.chip) + '</span>' : '') +
      (item.icon ? icon(item.icon) : '') +
      '<span class="name' +
      (item.mono ? ' mono' : '') +
      '">' +
      markRanges(item.label, match.ranges) +
      '</span><span class="r">' +
      (item.right ? esc(item.right) : '') +
      (item.keys ? kbd(item.keys) : '') +
      (item.href ? kbd(['Enter']).replace('class="kbd"', 'class="kbd ent"') : '') +
      '</span></button>';
  });
  html += '</div>';
  box.innerHTML = html;
  mark(app);
}

function mark(app: KrateDev): void {
  const st = state.get(app);
  if (!st || !app.pal) return;
  app.pal.querySelectorAll('.pi').forEach((el) => {
    const on = Number((el as HTMLElement).dataset.n) === st.sel;
    el.setAttribute('aria-selected', String(on));
    if (on) (el as HTMLElement).scrollIntoView?.({ block: 'nearest' });
  });
  app.pal.querySelector('input')?.setAttribute('aria-activedescendant', 'kd-pi-' + st.sel);
}

function run(app: KrateDev, item: PaletteItem | undefined, source: boolean): void {
  if (!item) return;
  if (source) {
    const file = item.source || (item.action === 'open-source' ? app.pageRoute().source : undefined);
    if (file) {
      closePalette(app);
      void app.openInEditor(file);
    }
    return;
  }
  closePalette(app);
  if (item.group === 'Routes') {
    if (!item.href) {
      app.toast('No prebuilt path for this route', item.label, 'x');
      return;
    }
    const base = app.cfg.basePath || '';
    const url = base + item.href;
    if (item.chip === 'API') window.open(url, '_blank', 'noopener');
    else location.href = url;
    return;
  }
  switch (item.action) {
    case 'inspect':
      app.inspector.toggle();
      break;
    case 'issues':
      app.openModal();
      break;
    case 'rebuild-page':
      void app.runAction('rebuild-page', { route: location.pathname });
      break;
    case 'rebuild-all':
    case 'restart-server':
    case 'clear-cache':
      void app.runAction(item.action);
      break;
    case 'open-source':
      void app.openInEditor(app.pageRoute().source);
      break;
    case 'settings':
      app.togglePanel('settings', true);
      break;
    case 'hide':
      app.toggleHidden();
      break;
  }
}

/** Keys while the menu is open; true when handled. */
export function paletteKey(app: KrateDev, e: KeyboardEvent): boolean {
  const st = state.get(app);
  if (!st) return false;
  if (e.altKey && e.code === 'KeyK') {
    e.preventDefault();
    closePalette(app);
    return true;
  }
  switch (e.key) {
    case 'Escape':
      e.preventDefault();
      e.stopPropagation();
      closePalette(app);
      return true;
    case 'ArrowDown':
      e.preventDefault();
      st.sel = Math.min(st.shown.length - 1, st.sel + 1);
      mark(app);
      return true;
    case 'ArrowUp':
      e.preventDefault();
      st.sel = Math.max(0, st.sel - 1);
      mark(app);
      return true;
    case 'Enter':
      e.preventDefault();
      run(app, st.shown[st.sel], e.metaKey || e.ctrlKey);
      return true;
  }
  return false;
}

export function paletteClick(app: KrateDev, act: string, t: HTMLElement, e: MouseEvent): boolean {
  if (act !== 'pal-run') return false;
  const st = state.get(app);
  run(app, st?.shown[Number(t.dataset.n)], e.metaKey || e.ctrlKey);
  return true;
}
