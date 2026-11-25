// The dev overlay controller: connection to the dev server, error capture,
// the dock and the layers it opens. Views live in panel.ts, modal.ts,
// palette.ts and inspector.ts; they render from the state kept here.

import { type ActivityData, type BuildRecord, DEFAULT_DOCS_URL, type DevInfo, type FixChange, type LogLine, type RouteData } from './api.js';
import type { RouteInfo } from './commands.js';
import { icon, mark } from './icons.js';
import {
  type CodeFrame,
  type DevClientError,
  type DevDiagnostic,
  type Issue,
  isBuildWarning,
  issueFromRuntime,
  issueFromServer,
  issuesFromDiagnostics,
} from './issues.js';
import { hostStackOf, parseFrames } from './native.js';
import { RequestLog, captureRequests, rawFetch } from './requests.js';
import { type Settings, kbd, loadSettings, nearestCorner, saveSettings, shortcutOf } from './settings.js';
import { symbolicateStack } from './sourcemap.js';
import { STYLE } from './styles.js';
import { basename, clock, devConfig, esc, fmtMs, formatArg, isTypingTarget, plural, routeMatches, type ResolvedConfig } from './util.js';
import { renderPanel, renderPanelBody, panelClick, type Tab, visibleTabs, refreshLogs, stopPanelTimers, startPanelTimers } from './panel.js';
import { renderModal, modalClick, modalKey, prepareIssue } from './modal.js';
import { openPalette, closePalette, paletteKey, paletteClick } from './palette.js';
import { Inspector } from './inspector.js';

const IGNORE_KEY = 'krate:dev-ignored';

export class KrateDev {
  cfg: ResolvedConfig = devConfig();
  settings: Settings = loadSettings();
  host!: HTMLElement;
  shadow!: ShadowRoot;
  root!: HTMLElement;
  dock?: HTMLElement;
  panel?: HTMLElement;
  toastEl?: HTMLElement;
  modal?: HTMLElement;
  pal?: HTMLElement;
  inspector = new Inspector(this);

  // connection and builds
  conn: 'connecting' | 'open' | 'down' = 'connecting';
  building = false;
  buildFiles: string[] = [];
  lastBuild?: BuildRecord;
  lastGoodAt?: number;
  builds: BuildRecord[] = [];
  activity: 'unknown' | 'ok' | 'missing' = 'unknown';
  info?: DevInfo | null;
  route?: RouteData | null;
  routeFor = '';
  routes?: RouteInfo[] | null;

  // issues
  issues: Issue[] = [];
  ignored = new Set<string>();
  cur = 0;
  modalOpen = false;
  seen = new Set<string>();
  frames = new Map<string, CodeFrame | null>();
  /** Log lines around a server crash, with their source and time. */
  tails = new Map<string, LogLine[]>();
  fixes = new Map<string, FixChange[] | null>();
  busy = new Set<string>();
  runtimeTab: 'stack' | 'console' | 'requests' = 'stack';
  /** Failed fetch/XHR requests of the page, for the runtime error's Request tab. */
  requests = new RequestLog();
  /**
   * The build failed and the error overlay was dismissed with "Show last good
   * page": the page on screen is the last good build until a build passes.
   */
  staleView = false;
  /** Registry entries of diagnostic codes (null: unknown), from /code. */
  codes = new Map<string, { title: string; explanation: string } | null>();
  /** The code whose "What this means" popover is open. */
  codePop = '';
  unfold = new Set<string>();

  // panel
  panelOpen = false;
  tab: Tab = 'route';
  hiddenAll = false;
  peek = false;

  // logs
  serverLogs: LogLine[] = [];
  localLogs: LogLine[] = [];
  logSeq = 0;
  logsMissing = false;
  logFilter: 'all' | 'server' | 'api' | 'browser' = 'all';
  logQuery = '';
  errorsOnly = false;
  logsClearedAt = 0;
  logsSeenAt = Date.now();
  logOpen = new Set<string>();
  localSeq = 0;

  // native
  hostSeq = 0;
  hostRows: string[] = [];

  timers: number[] = [];
  /** Undo functions for listeners outside the shadow root (destroy). */
  private unbind: (() => void)[] = [];
  sse?: EventSource;
  private toastTimer?: number;
  private restoreFocus?: HTMLElement | null;
  private suppressClick = false;

  init(): void {
    const host = document.createElement('krate-dev');
    host.id = 'krate-dev-root';
    host.style.cssText =
      'position:fixed!important;top:0!important;left:0!important;width:0!important;height:0!important;' +
      'overflow:visible!important;z-index:2147483000!important;pointer-events:none!important;display:block!important;' +
      'margin:0!important;padding:0!important;border:0!important;transform:none!important;filter:none!important';
    (document.body || document.documentElement).appendChild(host);
    this.host = host;
    this.shadow = host.attachShadow({ mode: 'open' });
    const style = document.createElement('style');
    style.textContent = STYLE;
    this.shadow.appendChild(style);
    this.root = document.createElement('div');
    this.root.className = 'kd';
    this.shadow.appendChild(this.root);
    this.applySettings();
    try {
      matchMedia('(prefers-color-scheme: light)').addEventListener('change', () => this.applySettings());
    } catch {
      /* without matchMedia, System stays dark */
    }
    try {
      for (const id of JSON.parse(sessionStorage.getItem(IGNORE_KEY) || '[]')) this.ignored.add(String(id));
    } catch {
      /* session storage is optional */
    }

    if (this.cfg.toolbar) {
      this.dock = document.createElement('div');
      this.dock.className = 'dock';
      this.dock.setAttribute('role', 'toolbar');
      this.dock.setAttribute('aria-label', 'Krate dev tools');
      this.root.appendChild(this.dock);
      this.initDrag(this.dock);
      this.dock.addEventListener('mouseenter', () => this.setPeek(true));
      this.dock.addEventListener('mouseleave', () => this.setPeek(false));
      this.watchLocation();
    }
    this.renderDock();

    this.shadow.addEventListener('click', (e) => this.onClick(e as MouseEvent));
    this.shadow.addEventListener('input', (e) => this.onInput(e));
    this.shadow.addEventListener('change', (e) => this.onInput(e));
    this.listen(document, 'keydown', (e) => this.onKey(e as KeyboardEvent), true);

    if (this.cfg.frames) this.connectFrames();
    else this.connectSSE();
    this.captureRuntimeErrors();
    this.unbind.push(captureRequests(this.requests));
    void this.loadStartup();
  }

  /** Stops timers and removes the overlay (tests). */
  private listen(target: EventTarget, type: string, fn: (e: Event) => void, capture = false): void {
    target.addEventListener(type, fn, capture);
    this.unbind.push(() => target.removeEventListener(type, fn, capture));
  }

  destroy(): void {
    for (const u of this.unbind.splice(0)) u();
    this.sse?.close();
    for (const t of this.timers) clearInterval(t);
    this.timers = [];
    stopPanelTimers(this);
    this.inspector.disable();
    this.host?.remove();
  }

  applySettings(): void {
    let theme = this.settings.theme;
    if (theme === 'system') {
      try {
        theme = matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
      } catch {
        theme = 'dark';
      }
    }
    this.host.setAttribute('data-theme', theme);
    this.root.className = 'kd c-' + this.settings.corner;
  }

  setSettings(patch: Partial<Settings>): void {
    this.settings = { ...this.settings, ...patch };
    saveSettings(this.settings);
    this.applySettings();
    this.renderDock();
    if (this.panelOpen) renderPanelBody(this);
  }

  // data

  apiURL(path: string, params?: Record<string, string | number | undefined>): string {
    let url = this.cfg.api.replace(/\/+$/, '') + path;
    if (params) {
      const q = Object.entries(params)
        .filter(([, v]) => v !== undefined && v !== '')
        .map(([k, v]) => encodeURIComponent(k) + '=' + encodeURIComponent(String(v)))
        .join('&');
      if (q) url += '?' + q;
    }
    return url;
  }

  /** GETs a dev endpoint; null when the host has no such endpoint or it fails. */
  async getJSON<T>(path: string, params?: Record<string, string | number | undefined>): Promise<T | null> {
    if (!this.cfg.api) return null;
    try {
      const res = await rawFetch(this.apiURL(path, params), { cache: 'no-store', headers: { Accept: 'application/json' } });
      if (!res.ok) return null;
      return (await res.json()) as T;
    } catch {
      return null;
    }
  }

  async postJSON(path: string, body: unknown): Promise<{ status: number; data: any }> {
    if (!this.cfg.api) return { status: 0, data: null };
    try {
      const res = await rawFetch(this.apiURL(path), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
        body: JSON.stringify(body),
      });
      let data: any = null;
      try {
        data = await res.json();
      } catch {
        data = null;
      }
      return { status: res.status, data };
    } catch {
      return { status: 0, data: null };
    }
  }

  async loadStartup(): Promise<void> {
    if (!this.cfg.api) {
      this.info = null;
      this.activity = 'missing';
      this.renderDock();
      return;
    }
    const [info, act] = await Promise.all([this.getJSON<DevInfo>('/info'), this.getJSON<ActivityData>('/activity')]);
    this.info = info;
    if (act && Array.isArray(act.builds)) {
      this.activity = 'ok';
      this.builds = act.builds.slice(-100);
      this.lastBuild = this.builds[this.builds.length - 1];
      this.lastGoodAt = act.lastGoodAt;
      if (act.building) this.building = true;
    } else {
      this.activity = 'missing';
    }
    if (!this.cfg.page) void this.loadRoute();
    this.renderDock();
    if (this.panelOpen) renderPanel(this);
  }

  async loadRoute(force = false): Promise<void> {
    const path = location.pathname;
    if (!force && this.routeFor === path && this.route !== undefined) return;
    this.routeFor = path;
    const r = await this.getJSON<RouteData>('/route', { path });
    if (this.routeFor !== path) return;
    this.route = r;
    this.renderDock();
    if (this.panelOpen && this.tab === 'route') renderPanelBody(this);
  }

  async loadRoutes(): Promise<RouteInfo[] | null> {
    const r = await this.getJSON<RouteInfo[]>('/routes');
    this.routes = Array.isArray(r) ? r : null;
    return this.routes;
  }

  /** The route pattern and mode of the page on screen. */
  pageRoute(): { route: string; mode: string; source?: string } {
    const path = location.pathname;
    if (this.route && this.routeFor === path) {
      return { route: this.route.pattern, mode: this.route.mode || '', source: this.route.source };
    }
    const p = this.cfg.page;
    if (p && (!this.routeFor || this.routeFor === path || routeMatches(p.route, path, this.cfg.basePath))) {
      return { route: p.route, mode: p.mode || '', source: p.source };
    }
    return { route: path, mode: '' };
  }

  // editor and actions

  async openInEditor(file: string | undefined, line?: number | string, col?: number | string): Promise<void> {
    if (!file) return;
    let url = this.cfg.open + '?file=' + encodeURIComponent(file) + '&line=' + encodeURIComponent(String(line || ''));
    if (col) url += '&col=' + encodeURIComponent(String(col));
    const ed = this.settings.editor;
    const known = ed && this.info?.editor?.available?.some((e) => e.id === ed);
    try {
      let res = await rawFetch(known ? url + '&editor=' + encodeURIComponent(ed) : url);
      if (!res.ok && known) res = await rawFetch(url);
      if (!res.ok) this.toast('Could not open the editor', file, 'x');
    } catch {
      this.toast('Could not open the editor', file, 'x');
    }
  }

  async runAction(action: string, extra: Record<string, unknown> = {}): Promise<boolean> {
    const { status } = await this.postJSON('/action', { action, ...extra });
    const labels: Record<string, string> = {
      'rebuild-page': 'Rebuilding this page',
      'rebuild-all': 'Rebuilding everything',
      'clear-cache': 'Cache cleared, rebuilding',
      'restart-server': 'Restarting the dev server',
      'restart-sidecar': 'Restarting the sidecar',
    };
    if (status === 202) {
      this.toast(labels[action] || 'Done', '', 'check');
      return true;
    }
    this.toast(status === 409 ? 'Not available right now' : status === 404 ? 'Nothing to do for this page' : 'The dev server refused', action, 'x');
    return false;
  }

  // dock

  renderDock(): void {
    const d = this.dock;
    if (!d) return;
    d.hidden = this.hiddenAll;
    const issues = this.visibleIssues();
    const warnings = issues.filter(isBuildWarning).length;
    const errors = issues.length - warnings;
    const mini = this.settings.collapsed && !this.peek;
    const inspecting = this.inspector.on;
    const down = this.conn === 'down';
    d.className =
      'dock' +
      (mini ? ' mini' : '') +
      (down ? ' off' : errors ? ' error' : warnings ? ' warn' : '') +
      (this.building && !down ? ' building' : '') +
      (inspecting ? ' inspect' : '');
    const markBtn =
      '<button class="markbtn" type="button" data-act="collapse" title="' +
      (this.settings.collapsed ? 'Expand the dock' : 'Collapse the dock') +
      '" aria-label="Krate">' +
      mark() +
      '</button>';
    if (mini) {
      d.innerHTML =
        markBtn +
        (errors
          ? '<i class="pip" aria-label="' + esc(plural(errors, 'issue')) + '"></i>'
          : warnings
            ? '<i class="pip w" aria-label="' + esc(plural(warnings, 'warning')) + '"></i>'
            : '');
      return;
    }
    if (down) {
      d.innerHTML =
        markBtn + '<div class="seg idle" role="status"><span>Reconnecting to dev server</span><span class="spin" style="margin-left:4px"></span></div>';
      return;
    }
    const { route, mode } = this.pageRoute();
    let html =
      markBtn +
      '<button class="seg' +
      (this.panelOpen ? ' open' : '') +
      '" type="button" data-act="panel" aria-expanded="' +
      this.panelOpen +
      '" title="Route details">' +
      (mode ? '<span class="mode ' + esc(mode) + '">' + esc(mode.toUpperCase()) + '</span>' : '') +
      '<span class="route mono">' +
      esc(route) +
      '</span></button><div class="sep"></div>';
    if (inspecting) {
      html +=
        '<button class="seg on" type="button" data-act="inspect" title="Stop inspecting">' +
        icon('mouse-pointer-2') +
        '<span>Inspecting</span><span class="kbd">Esc</span></button>';
    } else if (this.building) {
      const n = this.buildFiles.length;
      const what = n === 1 ? ' ' + basename(this.buildFiles[0]) : n > 1 ? ' ' + n + ' files' : '';
      html += '<div class="seg busy" role="status"><span>Rebuilding' + esc(what) + '</span></div>';
    } else if (errors) {
      const serverOnly = issues.every((i) => i.kind === 'server' || isBuildWarning(i));
      if (this.staleView && issues.some((i) => i.kind === 'build' && i.severity === 'error')) {
        html +=
          '<div class="seg stale" role="status" title="The build failed; this is the last build that passed">' +
          '<span>Last good build' +
          (this.lastGoodAt ? ' ' + esc(clock(this.lastGoodAt)) : '') +
          '</span></div>';
      }
      html +=
        '<button class="seg" type="button" data-act="issues" title="Show issues"><span class="badge err">' +
        esc(serverOnly ? 'Server down' : plural(errors, 'issue')) +
        '</span></button>';
    } else if (warnings) {
      html +=
        '<button class="seg" type="button" data-act="issues" title="Show build warnings"><span class="badge warn">' +
        esc(plural(warnings, 'warning')) +
        '</span></button>';
    } else if (this.lastBuild) {
      html +=
        '<button class="seg ok" type="button" data-act="activity" title="Last rebuild ' +
        esc(fmtMs(this.lastBuild.durationMs)) +
        '"><span class="ms mono">' +
        esc(fmtMs(this.lastBuild.durationMs)) +
        '</span></button>';
    } else {
      html += '<div class="seg ok"><span class="ms">Ready</span></div>';
    }
    if (!inspecting && !this.building && !errors) {
      html += '<button class="seg kbdseg" type="button" data-act="palette" title="Command menu">' + kbd(['Alt', 'K']) + '</button>';
    }
    d.innerHTML = html;
  }

  setPeek(on: boolean): void {
    if (!this.settings.collapsed || this.peek === on) return;
    this.peek = on;
    this.renderDock();
  }

  private initDrag(el: HTMLElement): void {
    let start: { x: number; y: number; dx: number; dy: number; id: number } | null = null;
    let dragging = false;
    el.addEventListener('pointerdown', (e) => {
      if (e.button !== 0) return;
      const r = el.getBoundingClientRect();
      start = { x: e.clientX, y: e.clientY, dx: e.clientX - r.left, dy: e.clientY - r.top, id: e.pointerId };
      dragging = false;
    });
    el.addEventListener('pointermove', (e) => {
      if (!start || e.pointerId !== start.id) return;
      if (!dragging && Math.hypot(e.clientX - start.x, e.clientY - start.y) < 5) return;
      if (!dragging) {
        dragging = true;
        try {
          el.setPointerCapture(e.pointerId);
        } catch {
          /* capture is a nicety */
        }
        el.classList.add('dragging');
      }
      el.style.left = Math.max(0, e.clientX - start.dx) + 'px';
      el.style.top = Math.max(0, e.clientY - start.dy) + 'px';
      el.style.right = 'auto';
      el.style.bottom = 'auto';
    });
    const end = (e: PointerEvent) => {
      if (!start || e.pointerId !== start.id) return;
      start = null;
      if (!dragging) return;
      dragging = false;
      const r = el.getBoundingClientRect();
      el.classList.remove('dragging');
      el.style.left = el.style.top = el.style.right = el.style.bottom = '';
      this.suppressClick = true;
      setTimeout(() => (this.suppressClick = false), 0);
      this.setSettings({ corner: nearestCorner(r.left + r.width / 2, r.top + r.height / 2, innerWidth, innerHeight) });
    };
    el.addEventListener('pointerup', end);
    el.addEventListener('pointercancel', end);
  }

  // layers

  togglePanel(tab?: Tab, force?: boolean): void {
    if (!this.cfg.toolbar) return;
    const open = force ?? (tab && this.panelOpen ? this.tab !== tab : !this.panelOpen);
    if (tab) this.tab = tab;
    if (!visibleTabs(this).includes(this.tab)) this.tab = 'route';
    this.panelOpen = open;
    if (open) {
      if (!this.panel) {
        this.panel = document.createElement('div');
        this.panel.className = 'panel';
        this.panel.setAttribute('role', 'dialog');
        this.panel.setAttribute('aria-label', 'Krate dev tools');
        this.root.insertBefore(this.panel, this.dock || null);
      }
      this.panel.hidden = this.hiddenAll;
      renderPanel(this);
      startPanelTimers(this);
      if (this.tab === 'route') void this.loadRoute();
    } else if (this.panel) {
      this.panel.hidden = true;
      stopPanelTimers(this);
    }
    this.renderDock();
  }

  setTab(tab: Tab): void {
    this.tab = tab;
    if (this.tab === 'logs') this.logsSeenAt = Date.now();
    renderPanel(this);
    startPanelTimers(this);
    if (tab === 'route') void this.loadRoute();
  }

  visibleIssues(): Issue[] {
    const rank = { build: 0, server: 1, runtime: 2 };
    return this.issues
      .filter((i) => !this.ignored.has(i.id) && (this.settings.showWarnings || !isBuildWarning(i)))
      .sort((a, b) => rank[a.kind] - rank[b.kind] || (a.severity === b.severity ? 0 : a.severity === 'error' ? -1 : 1));
  }

  openModal(): void {
    const list = this.visibleIssues();
    if (!list.length) return;
    if (!this.modalOpen) this.restoreFocus = document.activeElement as HTMLElement | null;
    this.modalOpen = true;
    if (this.cur >= list.length) this.cur = 0;
    if (!this.modal) {
      this.modal = document.createElement('div');
      this.modal.className = 'ov';
      this.root.appendChild(this.modal);
    }
    this.modal.hidden = this.hiddenAll;
    renderModal(this);
    prepareIssue(this, list[this.cur]);
    const m = this.modal.querySelector('.modal') as HTMLElement | null;
    try {
      m?.focus({ preventScroll: true });
    } catch {
      /* focus is a nicety */
    }
  }

  closeModal(): void {
    this.modalOpen = false;
    if (this.modal) this.modal.hidden = true;
    const f = this.restoreFocus;
    this.restoreFocus = null;
    try {
      if (f && f.isConnected) f.focus({ preventScroll: true });
    } catch {
      /* focus is a nicety */
    }
  }

  /** Re-renders whatever shows issues after the list changed. */
  issuesChanged(open: boolean): void {
    const list = this.visibleIssues();
    if (!list.length) {
      this.closeModal();
    } else if (this.modalOpen) {
      if (this.cur >= list.length) this.cur = list.length - 1;
      renderModal(this);
      prepareIssue(this, list[this.cur]);
    } else if (open && this.cfg.overlay) {
      this.cur = 0;
      this.openModal();
    }
    this.renderDock();
    if (this.panelOpen && this.tab === 'logs') refreshLogs(this);
  }

  ignore(id: string): void {
    this.ignored.add(id);
    try {
      sessionStorage.setItem(IGNORE_KEY, JSON.stringify(Array.from(this.ignored).slice(-50)));
    } catch {
      /* session storage is optional */
    }
    this.issuesChanged(false);
  }

  toggleHidden(): void {
    this.hiddenAll = !this.hiddenAll;
    for (const el of [this.dock, this.panel && this.panelOpen ? this.panel : undefined, this.modal && this.modalOpen ? this.modal : undefined])
      if (el) el.hidden = this.hiddenAll;
    if (this.hiddenAll) {
      this.inspector.disable();
      closePalette(this);
    }
    this.renderDock();
  }

  toast(text: string, meta = '', kind: 'check' | 'x' = 'check'): void {
    if (!this.toastEl) {
      this.toastEl = document.createElement('div');
      this.toastEl.className = 'toast';
      this.toastEl.setAttribute('role', 'status');
      this.root.appendChild(this.toastEl);
    }
    this.toastEl.innerHTML =
      icon(kind === 'x' ? 'x' : 'check', kind === 'x' ? 'x' : 'ok') +
      '<span>' +
      esc(text) +
      '</span>' +
      (meta ? '<span class="meta mono">' + esc(meta) + '</span>' : '');
    this.toastEl.hidden = this.hiddenAll || (this.panelOpen && !!this.panel && !this.panel.hidden);
    if (this.toastTimer) clearTimeout(this.toastTimer);
    this.toastTimer = setTimeout(() => {
      if (this.toastEl) this.toastEl.hidden = true;
    }, 2600) as unknown as number;
  }

  // events

  private onClick(e: MouseEvent): void {
    if (this.suppressClick) {
      e.stopPropagation();
      return;
    }
    const t = (e.target as HTMLElement).closest('[data-act]') as HTMLElement | null;
    if (!t) {
      // A click on the scrim closes the layer.
      if (e.target === this.modal) this.closeModal();
      if (e.target === this.pal) closePalette(this);
      return;
    }
    const act = t.getAttribute('data-act') || '';
    switch (act) {
      case 'collapse':
        this.peek = false;
        this.setSettings({ collapsed: !this.settings.collapsed });
        return;
      case 'panel':
        this.togglePanel('route');
        return;
      case 'activity':
        this.togglePanel(this.activity === 'ok' ? 'activity' : 'route');
        return;
      case 'issues':
        this.openModal();
        return;
      case 'palette':
        openPalette(this);
        return;
      case 'inspect':
        this.inspector.toggle();
        return;
      case 'open':
        void this.openInEditor(t.dataset.file, t.dataset.line, t.dataset.col);
        return;
    }
    if (panelClick(this, act, t, e)) return;
    if (modalClick(this, act, t, e)) return;
    paletteClick(this, act, t, e);
  }

  private onInput(e: Event): void {
    const t = e.target as HTMLInputElement | HTMLSelectElement;
    const name = t.getAttribute('data-input');
    if (name === 'logq') {
      this.logQuery = (t as HTMLInputElement).value;
      refreshLogs(this);
    } else if (name === 'editor' && e.type === 'change') {
      this.setSettings({ editor: t.value });
    }
  }

  private onKey(e: KeyboardEvent): void {
    if (this.pal && !this.pal.hidden && paletteKey(this, e)) return;
    const typing = isTypingTarget(e, this.host);
    const sc = shortcutOf(e);
    if (sc && !typing) {
      e.preventDefault();
      e.stopPropagation();
      if (sc === 'hide') this.toggleHidden();
      else if (this.hiddenAll) return;
      else if (sc === 'menu') openPalette(this);
      else if (sc === 'inspect') this.inspector.toggle();
      else if (sc === 'issues') {
        if (this.modalOpen) this.closeModal();
        else if (this.visibleIssues().length) this.openModal();
        else this.toast('No issues', '', 'check');
      }
      return;
    }
    if (this.modalOpen && modalKey(this, e, typing)) return;
    if (e.key === 'Escape' && !typing) {
      if (this.inspector.on) {
        this.inspector.disable();
        e.preventDefault();
      } else if (this.panelOpen) {
        this.togglePanel(undefined, false);
        e.preventDefault();
      }
    }
  }

  /** Keep the dock route label in sync with SPA (history) navigation. */
  private watchLocation(): void {
    let last = location.pathname;
    const update = () => {
      if (location.pathname === last) return;
      last = location.pathname;
      this.renderDock();
      if (this.cfg.api) void this.loadRoute(true);
      this.inspector.refresh();
    };
    this.listen(window, 'popstate', update);
    const h = history as any;
    const push = h.pushState.bind(history);
    h.pushState = function (...args: unknown[]) {
      const r = push(...args);
      update();
      return r;
    };
    const replace = h.replaceState.bind(history);
    h.replaceState = function (...args: unknown[]) {
      const r = replace(...args);
      update();
      return r;
    };
    // Safety net for routers that swap content without touching history.
    this.timers.push(setInterval(update, 500) as unknown as number);
  }

  private setConn(c: KrateDev['conn']): void {
    if (this.conn === c) return;
    this.conn = c;
    this.renderDock();
    if (this.panelOpen) renderPanel(this);
  }

  private connectSSE(): void {
    let sse: EventSource;
    try {
      sse = new EventSource(this.cfg.sse);
    } catch {
      return;
    }
    this.sse = sse;
    let connectedOnce = false;
    sse.addEventListener('connected', () => {
      // A second "connected" means the stream dropped and came back: the dev
      // server restarted (config or env change), so the page is stale.
      if (connectedOnce) location.reload();
      connectedOnce = true;
      this.setConn('open');
    });
    sse.addEventListener('open', () => this.setConn('open'));
    sse.addEventListener('error', () => {
      if (sse.readyState !== 1) this.setConn('down');
    });
    const parse = (e: Event): any => {
      try {
        return JSON.parse((e as MessageEvent).data);
      } catch {
        return null;
      }
    };
    sse.addEventListener('reload', (e) => this.onReload(parse(e)));
    sse.addEventListener('build-error', (e) => this.onBuildError(parse(e)));
    sse.addEventListener('build-warnings', (e) => this.onBuildWarnings(parse(e)));
    sse.addEventListener('client-error', (e) => this.onClientError(parse(e)));
    sse.addEventListener('build-start', (e) => this.onBuildStart(parse(e)));
    sse.addEventListener('build-end', (e) => this.onBuildEnd(parse(e)));
  }

  /**
   * Krate Native: the same events as SSE, as `{hmr: {type, ...}}` frames on a
   * streaming fetch. Reconnects after the stream ends; a second "connected"
   * means the shell restarted, so the page is stale.
   */
  private connectFrames(): void {
    let connectedOnce = false;
    const run = async (): Promise<void> => {
      const res = await rawFetch(this.cfg.frames, { cache: 'no-store' });
      if (!res.ok || !res.body) throw new Error('hmr stream unavailable');
      const reader = res.body.getReader();
      let buf: Uint8Array = new Uint8Array(0);
      for (;;) {
        const { done, value } = await reader.read();
        if (done) return;
        const joined = new Uint8Array(buf.length + value.length);
        joined.set(buf);
        joined.set(value, buf.length);
        const parsed = parseFrames(joined);
        buf = parsed.rest;
        for (const v of parsed.values) {
          const m = (v as { hmr?: any } | null)?.hmr;
          if (!m || typeof m !== 'object') continue;
          if (m.type === 'connected') {
            if (connectedOnce) location.reload();
            connectedOnce = true;
            this.setConn('open');
          } else if (m.type === 'reload') this.onReload(m);
          else if (m.type === 'build-error') this.onBuildError(m);
          else if (m.type === 'build-warnings') this.onBuildWarnings(m);
          else if (m.type === 'client-error') this.onClientError(m);
          else if (m.type === 'build-start') this.onBuildStart(m);
          else if (m.type === 'build-end') this.onBuildEnd(m);
        }
      }
    };
    const loop = (): void => {
      run().then(
        () => {
          this.setConn('down');
          setTimeout(loop, 1000);
        },
        () => {
          this.setConn('down');
          setTimeout(loop, 1000);
        },
      );
    };
    loop();
  }

  onBuildStart(d: any): void {
    this.building = true;
    this.buildFiles = d && Array.isArray(d.files) ? d.files : [];
    this.renderDock();
  }

  onBuildEnd(d: any): void {
    this.building = false;
    this.buildFiles = [];
    const b = d?.build as BuildRecord | undefined;
    if (b && b.kind !== 'noop') {
      this.lastBuild = b;
      this.builds.push(b);
      if (this.builds.length > 100) this.builds.shift();
      if (b.ok) this.lastGoodAt = b.start + b.durationMs;
      if (this.activity === 'unknown') this.activity = this.cfg.api ? 'ok' : 'missing';
      this.route = this.route && this.routeFor === location.pathname ? this.route : undefined;
      this.routeFor = '';
      this.routes = undefined;
      this.frames.clear();
      this.fixes.clear();
      if (b.ok) this.staleView = false;
      // Every build reports the warnings it leaves.
      this.setWarnings(d && Array.isArray(d.warnings) ? d.warnings : []);
    }
    this.renderDock();
    if (this.panelOpen && (this.tab === 'activity' || this.tab === 'route')) {
      renderPanelBody(this);
      if (this.tab === 'route') void this.loadRoute(true);
    }
    if (this.modalOpen) renderModal(this);
  }

  onReload(d: any): void {
    // A rebuild without diagnostics supersedes any compile error on screen,
    // whichever route it touched.
    this.staleView = false;
    if (this.issues.some((i) => i.kind === 'build' && i.severity === 'error')) {
      this.issues = this.issues.filter((i) => !(i.kind === 'build' && i.severity === 'error'));
      this.issuesChanged(false);
    }
    let cssOnly = false;
    if (d) {
      cssOnly = d.cssOnly === true;
      const pages = d.pages;
      if (
        pages &&
        Array.isArray(pages) &&
        !pages.some((p: string) => p === '/404' || p === '/500' || routeMatches(p, location.pathname, this.cfg.basePath))
      ) {
        return;
      }
    }
    if (cssOnly) {
      const b = this.lastBuild;
      this.swapStylesheets().then(
        () => {
          const files = b && b.kind === 'css-only' ? b.files.map(basename).slice(0, 2).join(', ') : '';
          const meta = [files, b && b.kind === 'css-only' ? fmtMs(b.durationMs) : ''].filter(Boolean).join(' \u00B7 ');
          this.toast('Styles updated without a reload', meta, 'check');
        },
        () => location.reload(),
      );
      return;
    }
    location.reload();
  }

  onBuildError(d: any): void {
    const diags: DevDiagnostic[] = d && Array.isArray(d.diagnostics) ? d.diagnostics : [];
    const warns: DevDiagnostic[] = d && Array.isArray(d.warnings) ? d.warnings : [];
    const next = [
      ...issuesFromDiagnostics(diags, Date.now()),
      ...issuesFromDiagnostics(
        warns.map((w) => ({ ...w, severity: 'warning' })),
        Date.now(),
        diags.length,
      ),
    ];
    this.replaceBuildIssues(next);
    // Only errors open the overlay; warnings wait in the dock.
    this.issuesChanged(next.some((i) => i.severity === 'error'));
  }

  /** The warnings of the latest build (build-end, build-warnings). */
  onBuildWarnings(d: any): void {
    this.setWarnings(d && Array.isArray(d.warnings) ? d.warnings : []);
  }

  setWarnings(list: DevDiagnostic[]): void {
    const next = issuesFromDiagnostics(
      list.map((w) => ({ ...w, severity: 'warning' })),
      Date.now(),
    );
    const before = this.issues.filter(isBuildWarning);
    if (before.length === next.length && before.every((b, k) => b.id === next[k].id && b.count === next[k].count)) return;
    this.issues = [...this.issues.filter((i) => !isBuildWarning(i)), ...next];
    for (const n of next) {
      const old = before.find((i) => i.id === n.id);
      if (old) n.time = old.time;
    }
    this.fixes.clear();
    this.issuesChanged(false);
  }

  /** Replaces every build issue (errors and warnings), keeping the times of those already on screen. */
  private replaceBuildIssues(next: Issue[]): void {
    for (const n of next) {
      const old = this.issues.find((i) => i.id === n.id);
      if (old) n.time = old.time;
    }
    this.issues = [...next, ...this.issues.filter((i) => i.kind !== 'build')];
    this.frames.clear();
    this.fixes.clear();
  }

  /** "Show last good page": the overlay closes and the dock says what is on screen. */
  showLastGood(): void {
    this.staleView = true;
    this.closeModal();
    this.renderDock();
  }

  onClientError(d: { clientError?: DevClientError } | null): void {
    if (!d || !d.clientError) return;
    const err = d.clientError;
    if (err.kind === 'server') {
      const issue = issueFromServer(err, Date.now());
      const old = this.issues.find((i) => i.id === issue.id);
      if (old) issue.count = old.count + 1;
      this.tails.delete(issue.id);
      this.frames.delete((issue.file || '') + ':' + (issue.line || 0));
      this.issues = [...this.issues.filter((i) => i.id !== issue.id), issue];
      this.localLog('error', '[' + (err.source || 'server') + '] ' + err.message);
      this.issuesChanged(true);
      return;
    }
    // This tab reported the error itself and already shows it.
    if (this.seen.has(err.message + '|' + (err.stack || ''))) return;
    this.addRuntime('remote', err.message, err.stack);
  }

  /**
   * Apply a CSS-only rebuild without a full reload: fetch the freshly built
   * document, add its stylesheet links, and drop the ones it no longer has.
   * Inline <style> changes cannot be diffed safely, so they fall back to a
   * reload (the caller reloads when this rejects).
   */
  async swapStylesheets(): Promise<void> {
    const res = await rawFetch(location.href, { cache: 'no-store', headers: { Accept: 'text/html' } });
    if (!res.ok) throw new Error('fetch failed');
    const next = new DOMParser().parseFromString(await res.text(), 'text/html');
    const inline = (d: Document) =>
      Array.from(d.querySelectorAll('style'))
        .map((el) => el.textContent || '')
        .join('\u0000');
    if (inline(next) !== inline(document)) throw new Error('inline styles changed');
    const abs = (href: string) => new URL(href, location.href).href;
    const want = Array.from(next.querySelectorAll('link[rel~="stylesheet"]')).map((l) => abs(l.getAttribute('href') || ''));
    const current = Array.from(document.querySelectorAll('link[rel~="stylesheet"]')) as HTMLLinkElement[];
    const have = new Set(current.map((l) => abs(l.getAttribute('href') || '')));
    const loads: Promise<void>[] = [];
    for (const href of want) {
      if (have.has(href)) continue;
      const link = document.createElement('link');
      link.rel = 'stylesheet';
      link.href = href;
      loads.push(
        new Promise<void>((resolve, reject) => {
          link.onload = () => resolve();
          link.onerror = () => reject(new Error('stylesheet failed'));
        }),
      );
      document.head.appendChild(link);
    }
    await Promise.all(loads);
    const keep = new Set(want);
    for (const l of current) {
      if (!keep.has(abs(l.getAttribute('href') || ''))) l.remove();
    }
    // A same-named stylesheet is cache-busted in place.
    for (const l of current) {
      const href = abs(l.getAttribute('href') || '');
      if (!keep.has(href) || !l.isConnected) continue;
      const u = new URL(href);
      u.searchParams.set('_krate', String(Date.now()));
      l.href = u.href;
    }
  }

  // runtime errors

  private captureRuntimeErrors(): void {
    this.listen(window, 'error', (e) => this.onError(e as ErrorEvent));
    this.listen(window, 'unhandledrejection', (e) => this.onRejection(e as PromiseRejectionEvent));
    // A script or stylesheet that fails to load fires on its element and does
    // not bubble, so only the capture phase sees it.
    this.listen(window, 'error', (e) => this.onResourceError(e), true);
    this.captureConsole();
    // This bundle loads after the page script, so the page's first render ran
    // before these listeners existed. The head buffers what happened until now.
    const early = (window as any).__KRATE_EARLY_ERRORS__ as any[] | undefined;
    (window as any).__KRATE_EARLY_ERRORS__ = undefined;
    for (const e of early ?? []) {
      if (e.type === 'console') this.onConsole(e.message as string, e.stack as string | undefined);
      else if (e.type === 'error' && e.target && e.target !== window) this.onResourceError(e as ErrorEvent);
      else if (e.type === 'error') this.onError(e as ErrorEvent);
      else this.onRejection(e as PromiseRejectionEvent);
    }
  }

  private captureConsole(): void {
    const c = console as any;
    const orig = c.error;
    c.error = (...args: unknown[]) => {
      try {
        const first = args.find((a) => a instanceof Error) as Error | undefined;
        this.onConsole(args.map(formatArg).join(' '), first?.stack);
      } catch {
        /* never break the page's own logging */
      }
      return orig.apply(c, args);
    };
    const origWarn = c.warn;
    c.warn = (...args: unknown[]) => {
      try {
        this.localLog('warn', 'console.warn ' + args.map(formatArg).join(' '));
      } catch {
        /* never break the page's own logging */
      }
      return origWarn.apply(c, args);
    };
    this.unbind.push(() => {
      c.error = orig;
      c.warn = origWarn;
    });
  }

  localLog(lvl: LogLine['lvl'], text: string): void {
    this.localLogs.push({ seq: ++this.localSeq, t: Date.now(), src: 'browser', lvl, text, local: true });
    if (this.localLogs.length > 300) this.localLogs.shift();
    if (this.panelOpen && this.tab === 'logs') refreshLogs(this);
    else if (this.panel && this.panelOpen) renderPanel(this, true);
  }

  private onConsole(message: string, stack?: string): void {
    this.report({ message, stack, url: location.href, kind: 'console' });
    this.localLog('error', 'console.error ' + message);
    if (this.settings.consoleIssues) this.addRuntime('console', message, stack);
  }

  private onResourceError(e: Event): void {
    const t = e.target as (HTMLElement & { src?: string; href?: string }) | null;
    if (!t || t === (window as any) || !t.tagName) return;
    const src = t.src || t.href || '';
    if (!src) return;
    const message = 'failed to load <' + t.tagName.toLowerCase() + '> ' + src;
    this.report({ message, url: src, kind: 'resource' });
    this.localLog('error', message);
    this.addRuntime('resource', message);
  }

  private onError(e: ErrorEvent): void {
    const stack = e.error?.stack || '';
    const message = e.message || 'Unknown error';
    const hostStack = hostStackOf(e.error);
    this.report({
      message,
      stack: hostStack ? stack + '\nHost stack:\n' + hostStack : stack,
      url: e.filename,
      line: e.lineno,
      col: e.colno,
      kind: 'error',
    });
    this.localLog('error', message);
    this.addRuntime('error', message, stack, hostStack);
  }

  private onRejection(e: PromiseRejectionEvent): void {
    const reason = e.reason;
    const message = reason instanceof Error ? reason.message : String(reason);
    const stack = reason instanceof Error ? reason.stack : '';
    const hostStack = hostStackOf(reason);
    this.report({ message, stack: hostStack ? (stack || '') + '\nHost stack:\n' + hostStack : stack, url: location.href, kind: 'unhandledrejection' });
    this.localLog('error', 'Unhandled rejection: ' + message);
    this.addRuntime('rejection', message, stack, hostStack);
  }

  private report(payload: DevClientError): void {
    const key = payload.message + '|' + (payload.stack || '');
    if (this.seen.has(key)) return;
    this.seen.add(key);
    const win = (globalThis as any).__KRATE_WINDOW__;
    if (typeof win === 'string' && win) payload.window = win;
    if (!payload.stack || !payload.stack.includes('.js')) {
      this.send(payload);
      return;
    }
    // Map the stack to the sources before it reaches the terminal, but never
    // hold the report back for long: the source maps are best-effort.
    const raw = payload.stack;
    Promise.race([symbolicateStack(raw), new Promise<string>((r) => setTimeout(() => r(raw), 1500))])
      .catch(() => raw)
      .then((stack) => {
        // The dev server echoes the report to every tab; this one knows it
        // by the mapped stack it sent.
        this.seen.add(payload.message + '|' + stack);
        this.send({ ...payload, stack });
      });
  }

  private send(payload: DevClientError): void {
    const body = JSON.stringify(payload);
    // sendBeacon throws on the custom scheme of a native webview (WebKitGTK),
    // so a failed or absent beacon falls back to fetch.
    try {
      if (!this.cfg.frames && navigator.sendBeacon) {
        if (navigator.sendBeacon(this.cfg.errors, new Blob([body], { type: 'application/json' }))) return;
      }
    } catch {
      /* fall through to fetch */
    }
    try {
      rawFetch(this.cfg.errors, { method: 'POST', body, headers: { 'Content-Type': 'application/json' }, keepalive: true }).catch(() => {});
    } catch {
      /* reporting is best-effort */
    }
  }

  addRuntime(origin: NonNullable<Issue['origin']>, message: string, stack?: string, hostStack?: string): void {
    const issue = issueFromRuntime(origin, message, stack, hostStack, Date.now());
    const old = this.issues.find((i) => i.id === issue.id);
    if (old) {
      old.count++;
      old.time = issue.time;
      this.issuesChanged(false);
      return;
    }
    this.issues.push(issue);
    if (this.issues.length > 60) {
      const drop = this.issues.findIndex((i) => i.kind === 'runtime');
      if (drop !== -1) this.issues.splice(drop, 1);
    }
    const open = origin !== 'console' && this.settings.openOnRuntime && !this.ignored.has(issue.id);
    this.issuesChanged(open);
    // Render immediately with the raw stack, then upgrade in place if a source
    // map lets us point at the original location.
    if (stack) {
      void symbolicateStack(stack).then((mapped) => {
        if (!mapped || mapped === stack) return;
        const upgraded = issueFromRuntime(origin, message, mapped, hostStack, issue.time);
        issue.stack = mapped;
        issue.file = upgraded.file;
        issue.line = upgraded.line;
        issue.col = upgraded.col;
        issue.served = upgraded.served;
        if (upgraded.label !== 'Error') issue.label = upgraded.label;
        this.issuesChanged(false);
      });
    }
  }

  docsURL(code: string): string {
    return (this.info?.docsURL || DEFAULT_DOCS_URL) + code.toLowerCase();
  }

  logFile(): string {
    return this.info?.logFile === undefined ? '.krate/dev.log' : this.info.logFile;
  }
}

