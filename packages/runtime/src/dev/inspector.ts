// The component inspector: tier outlines with names, a legend, and a hover
// tooltip. Clicks on the page open the hovered component's source.

import type { KrateDev } from './app.js';
import { icon } from './icons.js';
import {
  type Box,
  type CommentRange,
  type CompRow,
  TIERS,
  boxContains,
  commentRanges,
  elementBox,
  formatValue,
  hydrationOf,
  indexElements,
  kdNumbers,
  rangeBox,
  readTable,
  signalsOf,
  tierCounts,
} from './inspect.js';
import { renderPanelBody } from './panel.js';
import { copyText, esc, kb } from './util.js';

const TIER_NAME: Record<string, string> = { static: 'Static', server: 'Server', client: 'Client', runtime: 'Runtime', island: 'Island' };
const MAX_BOXES = 400;

export class Inspector {
  on = false;
  rows: CompRow[] = [];
  private els = new Map<number, Element[]>();
  private ranges = new Map<number, CommentRange>();
  private layer?: HTMLElement;
  private legend?: HTMLElement;
  private tip?: HTMLElement;
  private hot = -1;
  private frame = 0;
  private observer?: MutationObserver;
  private last: { x: number; y: number } | null = null;

  constructor(private app: KrateDev) {}

  private onMove = (e: PointerEvent) => {
    this.last = { x: e.clientX, y: e.clientY };
    this.hover(e.clientX, e.clientY, e.composedPath().includes(this.app.host));
  };

  private onClick = (e: MouseEvent) => {
    if (e.composedPath().includes(this.app.host)) return;
    const row = this.rows[this.hot];
    if (!row) return;
    e.preventDefault();
    e.stopPropagation();
    e.stopImmediatePropagation();
    if (!row.file) {
      this.app.toast('No source location for ' + row.name, '', 'x');
      return;
    }
    const loc = row.file + (row.line ? ':' + row.line : '');
    if (e.shiftKey) {
      void copyText(loc).then((ok) => this.app.toast(ok ? 'Path copied' : 'Could not copy', loc, ok ? 'check' : 'x'));
    } else {
      void this.app.openInEditor(row.file, row.line);
    }
  };

  /** Swallow the press that would start the click, so page handlers do not run. */
  private onDown = (e: PointerEvent | MouseEvent) => {
    if (e.composedPath().includes(this.app.host) || this.hot < 0) return;
    e.preventDefault();
    e.stopPropagation();
  };

  private onViewport = () => this.schedule();

  toggle(): void {
    if (this.on) this.disable();
    else this.enable();
  }

  enable(): void {
    const rows = readTable();
    if (!rows) {
      this.app.toast('No component data on this page', 'dev builds only', 'x');
      return;
    }
    if (this.app.hiddenAll) return;
    this.on = true;
    this.rows = rows;
    this.index();
    const root = this.app.root;
    this.layer = document.createElement('div');
    this.layer.className = 'ins';
    this.layer.setAttribute('aria-hidden', 'true');
    root.insertBefore(this.layer, root.firstChild);
    this.legend = document.createElement('div');
    this.legend.className = 'legend';
    this.legend.setAttribute('role', 'status');
    root.appendChild(this.legend);
    this.tip = document.createElement('div');
    this.tip.className = 'tip';
    this.tip.hidden = true;
    root.appendChild(this.tip);
    this.renderLegend();
    this.draw();
    document.addEventListener('pointermove', this.onMove, true);
    document.addEventListener('click', this.onClick, true);
    document.addEventListener('pointerdown', this.onDown, true);
    document.addEventListener('mousedown', this.onDown, true);
    window.addEventListener('scroll', this.onViewport, true);
    window.addEventListener('resize', this.onViewport);
    try {
      this.observer = new MutationObserver((list) => {
        if (list.some((m) => !this.app.host.contains(m.target))) {
          this.index();
          this.schedule();
        }
      });
      this.observer.observe(document.body, { childList: true, subtree: true, attributes: true, attributeFilter: ['data-kd', 'style', 'class'] });
    } catch {
      /* without an observer the outlines follow scroll and resize only */
    }
    this.app.renderDock();
    if (this.app.panelOpen && this.app.tab === 'inspect') renderPanelBody(this.app);
    if (this.app.cfg.api && this.app.route === undefined) void this.app.loadRoute();
  }

  disable(): void {
    if (!this.on) return;
    this.on = false;
    this.hot = -1;
    document.removeEventListener('pointermove', this.onMove, true);
    document.removeEventListener('click', this.onClick, true);
    document.removeEventListener('pointerdown', this.onDown, true);
    document.removeEventListener('mousedown', this.onDown, true);
    window.removeEventListener('scroll', this.onViewport, true);
    window.removeEventListener('resize', this.onViewport);
    this.observer?.disconnect();
    this.observer = undefined;
    if (this.frame) cancelAnimationFrame(this.frame);
    this.frame = 0;
    this.layer?.remove();
    this.legend?.remove();
    this.tip?.remove();
    this.layer = this.legend = this.tip = undefined;
    this.app.renderDock();
    if (this.app.panelOpen && this.app.tab === 'inspect') renderPanelBody(this.app);
  }

  /** Re-reads the table after a client-side navigation. */
  refresh(): void {
    if (!this.on) return;
    this.rows = readTable() || [];
    this.index();
    this.renderLegend();
    this.schedule();
  }

  private index(): void {
    this.els = indexElements();
    this.ranges = commentRanges(this.rows);
  }

  private schedule(): void {
    if (this.frame) return;
    this.frame = requestAnimationFrame(() => {
      this.frame = 0;
      this.draw();
      if (this.last) this.hover(this.last.x, this.last.y, false);
    });
  }

  /** The boxes an instance covers on screen. */
  boxesOf(row: CompRow): Box[] {
    const out: Box[] = [];
    for (const el of this.els.get(row.n) || []) {
      const b = elementBox(el);
      if (b) out.push(b);
    }
    const rg = this.ranges.get(row.n);
    if (rg) {
      const b = rangeBox(rg);
      if (b) out.push(b);
    }
    return out;
  }

  renderLegend(): void {
    if (!this.legend) return;
    const c = tierCounts(this.rows);
    const isl = this.app.route?.sizes?.gzipIslands;
    this.legend.innerHTML =
      TIERS.filter((t) => t !== 'island' || c.island)
        .map((t) => '<div class="li tp ' + t + '">' + TIER_NAME[t] + ' <b>' + c[t] + '</b></div>')
        .join('') +
      (isl ? '<span class="sep"></span><span class="js">' + kb(isl) + ' KB of island JS</span>' : '') +
      '<button class="iconbtn" type="button" data-act="inspect" aria-label="Stop inspecting">' +
      icon('x') +
      '</button>';
  }

  private draw(): void {
    if (!this.layer) return;
    const vw = innerWidth;
    const vh = innerHeight;
    // Instances that share a root element draw one outline, labeled with the
    // innermost name (rows come outermost first).
    const boxes: { row: CompRow; box: Box; label: boolean }[] = [];
    const seen = new Map<string, number>();
    for (const row of this.rows) {
      if (boxes.length >= MAX_BOXES) break;
      this.boxesOf(row).forEach((box, k) => {
        if (box.top > vh || box.left > vw || box.top + box.height < 0 || box.left + box.width < 0) return;
        const key = [box.left, box.top, box.width, box.height].map(Math.round).join(',');
        const at = seen.get(key);
        if (at !== undefined) {
          boxes[at].row = row;
          return;
        }
        seen.set(key, boxes.length);
        boxes.push({ row, box, label: k === 0 });
      });
    }
    this.layer.innerHTML = boxes
      .map(({ row, box: b, label }) => {
        const cls = 'ob ' + row.tier + (b.left + 140 > vw ? ' flip' : '') + (b.top < 24 ? ' inside' : '');
        return (
          '<div class="' +
          cls +
          '" data-n="' +
          row.n +
          '" style="left:' +
          b.left +
          'px;top:' +
          b.top +
          'px;width:' +
          b.width +
          'px;height:' +
          b.height +
          'px">' +
          (label ? '<span class="nm">' + esc(row.name) + '</span>' : '') +
          '</div>'
        );
      })
      .join('');
    this.markHot();
  }

  private markHot(): void {
    this.layer?.querySelectorAll('.ob').forEach((el) => el.classList.toggle('hot', Number((el as HTMLElement).dataset.n) === this.hot));
  }

  /** The innermost instance at a point. */
  rowAt(x: number, y: number): number {
    let el = document.elementFromPoint(x, y);
    let best = -1;
    let bestArea = Infinity;
    while (el && el !== document.documentElement) {
      if (el === this.app.host) return -1;
      const ns = kdNumbers(el);
      if (ns.length) {
        best = ns[ns.length - 1];
        const b = elementBox(el);
        bestArea = b ? b.width * b.height : Infinity;
        break;
      }
      el = el.parentElement;
    }
    // Comment-delimited instances (runtime regions, "use client" islands)
    // have no element; take one when it is smaller than the element found.
    for (const [n] of this.ranges) {
      const row = this.rows[n];
      if (!row) continue;
      for (const b of this.boxesOf(row)) {
        if (boxContains(b, x, y) && b.width * b.height < bestArea) {
          best = n;
          bestArea = b.width * b.height;
        }
      }
    }
    return best;
  }

  private hover(x: number, y: number, overOverlay: boolean): void {
    const n = overOverlay ? -1 : this.rowAt(x, y);
    if (n !== this.hot) {
      this.hot = n;
      this.markHot();
      this.renderTip();
    }
    this.placeTip(x, y);
  }

  private renderTip(): void {
    const tip = this.tip;
    if (!tip) return;
    const row = this.rows[this.hot];
    if (!row) {
      tip.hidden = true;
      return;
    }
    let kv = '';
    if (row.file) kv += '<dt>Source</dt><dd><span class="mono link path">' + esc(row.file + (row.line ? ':' + row.line : '')) + '</span></dd>';
    const props = this.ranges.get(row.n)?.data?.props;
    if (props && typeof props === 'object' && Object.keys(props).length) kv += '<dt>Props</dt><dd><span class="mono">' + esc(formatValue(props)) + '</span></dd>';
    const sigs = signalsOf(row.key);
    if (sigs.length) {
      kv +=
        '<dt>Signals</dt><dd><span class="mono">' +
        sigs
          .map((s) => {
            const cls = typeof s.value === 'boolean' ? 'sv-b' : typeof s.value === 'string' ? 'sv-s' : 'sv-n';
            return esc(s.name) + ' = <span class="' + cls + '">' + esc(formatValue(s.value)) + '</span>';
          })
          .join(', ') +
        '</span></dd>';
    }
    const gz = row.island ? this.app.route?.islands?.find((i) => i.id === row.island)?.gzip : undefined;
    if (gz !== undefined) kv += '<dt>Shipped</dt><dd><span class="mono">' + kb(gz) + ' KB gz</span></dd>';
    else if (row.tier === 'static' || row.tier === 'server') kv += '<dt>Shipped</dt><dd><span class="mono">HTML only, no JS</span></dd>';
    const ms = hydrationOf(row);
    if (ms !== undefined) kv += '<dt>Hydrated</dt><dd><span class="mono">' + (ms < 10 ? ms.toFixed(1) : Math.round(ms)) + ' ms</span></dd>';
    if (row.tier === 'runtime') kv += '<dt>Renders</dt><dd><span class="mono">per request</span></dd>';
    tip.innerHTML =
      '<div class="tip-h"><span class="name">' +
      esc(row.name) +
      '</span><span class="tp ' +
      row.tier +
      '">' +
      row.tier +
      '</span></div>' +
      (kv ? '<div class="tip-b"><dl class="kv">' + kv + '</dl></div>' : '') +
      '<div class="tip-f"><span>Click: open in editor</span><span>Shift-click: copy path</span></div>';
    tip.hidden = false;
  }

  private placeTip(x: number, y: number): void {
    const tip = this.tip;
    if (!tip || tip.hidden) return;
    const w = tip.offsetWidth || 300;
    const h = tip.offsetHeight || 160;
    let left = x + 16;
    let top = y + 18;
    if (left + w > innerWidth - 8) left = Math.max(8, x - w - 16);
    if (top + h > innerHeight - 8) top = Math.max(8, y - h - 18);
    tip.style.left = left + 'px';
    tip.style.top = top + 'px';
  }
}
