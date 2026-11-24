import { afterEach, describe, expect, it } from 'vitest';
import { filterItems, fuzzyMatch, markRanges, routeItems, type RouteInfo } from './commands.js';
import { highlightLine, wordAt } from './highlight.js';
import { commentRanges, componentPath, hydrationOf, readTable, signalsOf, tierCounts, type CompRow } from './inspect.js';
import { cleanMessage, copyForAI, frameText, issueFromDiagnostic, issueFromRuntime, issueFromServer, issueSummary } from './issues.js';
import { buildDetail, buildHeadline, filterLogs, headChecks, pageIslands } from './panel.js';
import { DEFAULT_SETTINGS, SETTINGS_KEY, kbd, loadSettings, nearestCorner, saveSettings, shortcutOf } from './settings.js';
import { applyChange } from './modal.js';
import { errorName, firstAppFrame, isFrameworkFile, parseFrame, parseStack, projectLocation, splitStack } from './stack.js';
import { fmtMs, isTypingTarget, kb, routeMatches, stripBase } from './util.js';
import type { BuildRecord, LogLine } from './api.js';

const V8 = `TypeError: Cannot read properties of undefined (reading 'count')
    at load (src/components/LikeButton.tsx:22:31)
    at o (http://localhost:3000/chunks/runtime.ehntno.js:1:17593)
    at pl (http://localhost:3000/chunks/runtime.ehntno.js:1:17756)
    at LikeButton (src/components/LikeButton.tsx:9:3)
    at x (/repo/node_modules/preact/dist/preact.mjs:1:200)
    at PostPage (src/pages/blog/[slug].tsx:31:7)`;

describe('formatting', () => {
  it('formats sizes and durations', () => {
    expect(kb(14540)).toBe('14.2');
    expect(kb(30720)).toBe('30');
    expect(kb(0)).toBe('0');
    expect(kb(300 * 1024)).toBe('300');
    expect(fmtMs(184.3)).toBe('184ms');
    expect(fmtMs(0.9)).toBe('0.9ms');
    expect(fmtMs(2900)).toBe('2.9s');
    expect(fmtMs(undefined)).toBe('');
  });

  it('matches routes with the base path stripped', () => {
    expect(stripBase('/docs/a', '/docs')).toBe('/a');
    expect(routeMatches('/blog/[slug]', '/blog/first/')).toBe(true);
    expect(routeMatches('/blog/[slug]', '/blog')).toBe(false);
    expect(routeMatches('/docs/[...all]', '/docs/a/b/c')).toBe(true);
    expect(routeMatches('/a', '/base/a', '/base')).toBe(true);
  });
});

describe('stack', () => {
  it('parses V8 and Firefox frames', () => {
    expect(parseFrame('    at load (src/a.tsx:22:31)')).toMatchObject({ fn: 'load', file: 'src/a.tsx', line: 22, col: 31, app: true, served: false });
    const anon = parseFrame('    at http://localhost:3000/oops/index.js:15:23');
    expect(anon).toMatchObject({ fn: '<anonymous>', line: 15, col: 23, served: true });
    expect(parseFrame('load@http://x.test/chunks/runtime.js:1:2')).toMatchObject({ fn: 'load', app: false });
    expect(parseFrame('TypeError: boom')).toBeNull();
  });

  it('splits app frames from folded framework frames', () => {
    const frames = parseStack(V8);
    expect(frames).toHaveLength(6);
    const items = splitStack(frames);
    expect(items.map((i) => (i.type === 'app' ? 'app:' + i.frame.fn : 'fold:' + i.frames.length))).toEqual([
      'app:load',
      'fold:2',
      'app:LikeButton',
      'fold:1',
      'app:PostPage',
    ]);
    expect(firstAppFrame(frames)?.file).toBe('src/components/LikeButton.tsx');
    expect(isFrameworkFile('node_modules/preact/x.js')).toBe(true);
    expect(isFrameworkFile('src/pages/index.tsx')).toBe(false);
  });

  it('stops at the host stack', () => {
    expect(parseStack('Error: x\n    at a (src/a.ts:1:1)\nHost stack:\n    at b (src/b.ts:1:1)')).toHaveLength(1);
  });

  it('names the error class', () => {
    expect(errorName(V8)).toBe('TypeError');
    expect(errorName('', 'ReferenceError: window is not defined')).toBe('ReferenceError');
    expect(errorName('boom')).toBe('');
  });

  it('finds a project location in a server stack, ignoring URLs and dependencies', () => {
    const text =
      '[browser] Uncaught TypeError http://localhost:3000/oops/index.js:15:23\n' +
      'ReferenceError: window is not defined\n    at /app/node_modules/chart.js/a.js:1:1\n    at src/lib/chart.ts:3:15';
    expect(projectLocation(text)).toEqual({ file: 'src/lib/chart.ts', line: 3, col: 15 });
    expect(projectLocation('nothing here')).toBeNull();
  });
});

describe('issues', () => {
  const diag = {
    file: 'src/components/LikeButton.tsx',
    line: 14,
    col: 7,
    code: 'KR5002',
    message: 'invalid event handler `onclick` on <button>',
    hint: 'rename it to onClick',
  };

  it('builds issues from diagnostics, runtime errors and server crashes', () => {
    const b = issueFromDiagnostic(diag, 0, 1);
    expect(b).toMatchObject({ kind: 'build', label: 'KR5002', severity: 'error', file: diag.file });
    expect(issueFromDiagnostic({ message: 'w', severity: 'warning' }, 1, 1)).toMatchObject({ severity: 'warning', label: 'Warning' });
    const r = issueFromRuntime('error', "Uncaught TypeError: Cannot read properties of undefined (reading 'count')", V8, undefined, 1);
    expect(r).toMatchObject({ label: 'TypeError', file: 'src/components/LikeButton.tsx', line: 22, col: 31 });
    expect(r.message).toBe("Cannot read properties of undefined (reading 'count')");
    expect(issueFromRuntime('console', 'oops', undefined, undefined, 1)).toMatchObject({ label: 'console.error', severity: 'warning' });
    const s = issueFromServer({ message: 'SSR renderer failed: exited', kind: 'server', source: 'SSR renderer', detail: 'exited', exitCode: 1, restart: 'ssr' }, 1);
    expect(s).toMatchObject({ id: 's:SSR renderer', kind: 'server', label: 'SSR renderer', message: 'exited' });
    const withTail = issueFromServer(
      { message: 'x', kind: 'server', source: 'SSR renderer', stack: 'old src/a.tsx:1:1 line\n', log: ['old src/a.tsx:1:1 line'] },
      1,
    );
    expect(withTail.file).toBeUndefined();
    expect(issueSummary([b, { ...b, id: 'x', file: 'src/b.tsx' }])).toBe('2 issues in 2 files');
    expect(cleanMessage('Uncaught (in promise) Error: no', 'Error')).toBe('no');
  });

  it('writes copy-for-AI markdown with location, frame, hint and stack', () => {
    const issue = issueFromDiagnostic(diag, 0, 1);
    const frame = { file: diag.file, lang: 'tsx', start: 13, lines: ['      class="like"', '      onclick={toggle}', '    >'] };
    const md = copyForAI(issue, frame, { url: '/blog/first', route: '/blog/[slug]' });
    expect(md).toContain('## Build error KR5002: invalid event handler `onclick` on <button>');
    expect(md).toContain('- Location: `src/components/LikeButton.tsx:14:7`');
    expect(md).toContain('- Page: /blog/first (route `/blog/[slug]`)');
    expect(md).toContain('```tsx\n  13 |       class="like"\n> 14 |       onclick={toggle}\n     |       ^\n  15 |     >\n```');
    expect(md).toContain('Hint: rename it to onClick');
    expect(md).not.toContain('Stack:');
    const rt = copyForAI(issueFromRuntime('error', 'boom', V8, 'host frames', 1), null);
    expect(rt).toContain('Stack:');
    expect(rt).toContain('Host stack:');
    expect(frameText(frame)).not.toMatch(/^> /m);
  });
});

describe('highlight', () => {
  it('escapes and colors a TSX line', () => {
    const html = highlightLine('<button class="a" onClick={go}>x</button>', 'tsx');
    expect(html).toContain('<span class="k1">button</span>');
    expect(html).toContain('<span class="k3">&quot;a&quot;</span>');
    expect(html).not.toContain('<button');
  });

  it('underlines the word at the column', () => {
    const src = '      onclick={toggle}';
    expect(wordAt(src, 7)).toEqual([6, 13]);
    expect(highlightLine(src, 'tsx', wordAt(src, 7))).toContain('sq">onclick</span>');
    expect(highlightLine('aria-pressed={x}', 'tsx')).toBe('<span class="k2">aria-pressed</span>={<span class="k6">x</span>}');
  });

  it('applies a fix to its column', () => {
    const c = { file: 'a', line: 1, col: 7, code: 'KR5002', from: 'onclick', to: 'onClick' };
    expect(applyChange('      onclick={toggle}', c)).toBe('      onClick={toggle}');
    expect(applyChange('x onclick', { ...c, col: 99 })).toBe('x onClick');
  });
});

describe('command menu', () => {
  const routes: RouteInfo[] = [
    { pattern: '/', source: 'src/pages/index.tsx', mode: 'ssg', kind: 'page', paths: ['/'] },
    { pattern: '/blog', source: 'src/pages/blog/index.tsx', mode: 'ssg', kind: 'page', paths: ['/blog'] },
    { pattern: '/blog/[slug]', source: 'src/pages/blog/[slug].tsx', mode: 'ssg', kind: 'page', paths: ['/blog/first', '/blog/second'] },
    { pattern: '/pricing', source: 'src/pages/pricing.tsx', mode: 'isr', revalidate: 60, kind: 'page', paths: ['/pricing'] },
    { pattern: '/api/go/thing', kind: 'api', methods: ['GET', 'POST'], paths: ['/api/go/thing'] },
  ];

  it('scores substring matches above subsequences and marks them', () => {
    const a = fuzzyMatch('blog', '/blog/[slug]')!;
    const b = fuzzyMatch('bg', '/blog/[slug]')!;
    expect(a.score).toBeGreaterThan(b.score);
    expect(fuzzyMatch('zzz', '/blog')).toBeNull();
    expect(markRanges('/blog', a.ranges)).toBe('/<mark>blog</mark>');
  });

  it('describes routes and lists concrete paths of dynamic routes only for a query', () => {
    const empty = routeItems(routes, '');
    expect(empty.map((i) => i.label)).toEqual(['/', '/blog', '/blog/[slug]', '/pricing', '/api/go/thing']);
    expect(empty[2]).toMatchObject({ right: '2 paths', href: '/blog/first', chip: 'SSG' });
    expect(empty[3].right).toBe('revalidate 60s');
    expect(empty[4]).toMatchObject({ chip: 'API', right: 'GET POST' });
    const q = filterItems(routeItems(routes, 'second'), 'second');
    expect(q.map((r) => r.item.label)).toEqual(['/blog/second']);
  });

  it('orders routes before actions and filters both', () => {
    const items = [...routeItems(routes, 'blog'), { id: 'a', group: 'Actions' as const, label: 'Rebuild this page' }];
    const res = filterItems(items, 'blog');
    expect(res[0].item.label).toBe('/blog');
    expect(res.every((r) => r.item.group === 'Routes')).toBe(true);
    expect(filterItems(items, 'rebuild').map((r) => r.item.label)).toEqual(['Rebuild this page']);
  });
});

describe('settings', () => {
  afterEach(() => localStorage.clear());

  it('persists and validates settings', () => {
    expect(loadSettings()).toEqual(DEFAULT_SETTINGS);
    saveSettings({ ...DEFAULT_SETTINGS, corner: 'tr', theme: 'light', editor: 'zed', collapsed: true });
    expect(loadSettings()).toMatchObject({ corner: 'tr', theme: 'light', editor: 'zed', collapsed: true });
    localStorage.setItem(SETTINGS_KEY, '{"corner":"middle","theme":"neon","editor":"code --evil","openOnRuntime":"yes"}');
    expect(loadSettings()).toEqual(DEFAULT_SETTINGS);
    localStorage.setItem(SETTINGS_KEY, 'not json');
    expect(loadSettings()).toEqual(DEFAULT_SETTINGS);
  });

  it('snaps to the nearest corner', () => {
    expect(nearestCorner(10, 10, 1000, 800)).toBe('tl');
    expect(nearestCorner(900, 700, 1000, 800)).toBe('br');
  });

  it('matches shortcuts by physical key', () => {
    const ev = (code: string, mods: KeyboardEventInit = {}) => new KeyboardEvent('keydown', { code, altKey: true, ...mods });
    expect(shortcutOf(ev('KeyK'))).toBe('menu');
    expect(shortcutOf(ev('KeyI'))).toBe('inspect');
    expect(shortcutOf(ev('KeyE'))).toBe('issues');
    expect(shortcutOf(ev('Period'))).toBe('hide');
    expect(shortcutOf(ev('KeyK', { ctrlKey: true }))).toBeNull();
    expect(shortcutOf(new KeyboardEvent('keydown', { code: 'KeyK' }))).toBeNull();
  });

  it('labels modifiers with icons on macOS and text elsewhere', () => {
    expect(kbd(['Alt', 'K'], false)).toBe('<span class="kbd">Alt K</span>');
    const mac = kbd(['Alt', 'K'], true);
    expect(mac).toContain('<svg');
    expect(mac).not.toContain('Alt');
  });

  it('knows when the user is typing in the page', () => {
    const input = document.createElement('input');
    const button = document.createElement('input');
    button.type = 'checkbox';
    const div = document.createElement('div');
    div.contentEditable = 'true';
    document.body.append(input, button, div);
    const at = (el: Element) => {
      const e = new KeyboardEvent('keydown', { bubbles: true });
      Object.defineProperty(e, 'target', { value: el });
      return isTypingTarget(e);
    };
    expect(at(input)).toBe(true);
    expect(at(button)).toBe(false);
    expect(at(div)).toBe(true);
    expect(at(document.body)).toBe(false);
    input.remove();
    button.remove();
    div.remove();
  });
});

describe('inspector data', () => {
  const rows: CompRow[] = [
    { n: 0, name: 'RootLayout', tier: 'static', parent: -1, file: 'src/pages/_layout.tsx' },
    { n: 1, name: 'PostPage', tier: 'server', parent: 0 },
    { n: 2, key: 'k.Like', name: 'LikeButton', tier: 'client', parent: 1 },
    { n: 3, key: 'k.Inner', name: 'Inner', tier: 'client', parent: 2 },
    { n: 4, name: 'ViewCount', tier: 'runtime', parent: 1, region: 'region-1' },
    { n: 5, name: 'Chart', tier: 'island', parent: 1, island: 'i3' },
  ];

  afterEach(() => {
    document.body.innerHTML = '';
    delete (globalThis as any).__krate_dev_signals;
    delete (globalThis as any).__krate_dev_hydration;
    delete (globalThis as any).__krate_island_hydration;
  });

  it('reads the table and counts tiers', () => {
    expect(readTable()).toBeNull();
    document.body.innerHTML = '<script type="application/json" id="__krate_dev_components">' + JSON.stringify(rows) + '</script>';
    expect(readTable()).toHaveLength(6);
    expect(tierCounts(rows)).toEqual({ static: 1, server: 1, client: 2, runtime: 1, island: 1 });
    expect(componentPath(rows, rows[3])).toEqual(['RootLayout', 'PostPage', 'LikeButton', 'Inner']);
    expect(pageIslands(rows).map((r) => r.name)).toEqual(['LikeButton', 'ViewCount', 'Chart']);
  });

  it('reads live signals and hydration times', () => {
    let n = 1;
    (globalThis as any).__krate_dev_signals = [
      { c: 'k.Like', name: 'count', get: () => n },
      { c: 'k.Like', name: 'liked', get: () => false },
      { c: 'other', name: 'x', get: () => 0 },
    ];
    (globalThis as any).__krate_dev_hydration = { 'k.Like': 0.9 };
    (globalThis as any).__krate_island_hydration = { i3: { ms: 2.5 } };
    n = 128;
    expect(signalsOf('k.Like')).toEqual([
      { name: 'count', value: 128 },
      { name: 'liked', value: false },
    ]);
    expect(hydrationOf(rows[2])).toBe(0.9);
    expect(hydrationOf(rows[5])).toBe(2.5);
    expect(hydrationOf(rows[0])).toBeUndefined();
  });

  it('finds comment-delimited regions and islands', () => {
    document.body.innerHTML =
      '<main><!--region:region-1--><div>views</div><!--/region:region-1-->' +
      '<!--krate-island:{"props":{"id":1}}--><p>a</p><!--krate-island:{}--><b>n</b><!--/krate-island--><!--/krate-island--></main>';
    const r = commentRanges([...rows, { n: 6, name: 'Tabs', tier: 'island', parent: 1, boundary: 0 }]);
    expect(r.get(4)?.end.data).toBe('/region:region-1');
    const isl = r.get(6)!;
    expect(isl.data).toEqual({ props: { id: 1 } });
    expect(isl.end.nextSibling).toBeNull();
  });
});

describe('panel helpers', () => {
  it('checks the head', () => {
    document.title = '';
    document.documentElement.removeAttribute('lang');
    let checks = headChecks();
    expect(checks.filter((c) => !c.ok).map((c) => c.missing)).toEqual(['no title', 'no description', 'no og:image', 'no canonical', 'no lang on <html>']);
    document.title = 'Hi';
    document.documentElement.setAttribute('lang', 'en');
    document.head.insertAdjacentHTML('beforeend', '<meta name="description" content="d"><link rel="canonical" href="/x">');
    checks = headChecks();
    expect(checks.filter((c) => c.ok).map((c) => c.name)).toEqual(['title', 'description', 'canonical', 'lang']);
    document.head.innerHTML = '';
  });

  it('describes builds', () => {
    const rec = (p: Partial<BuildRecord>): BuildRecord => ({ id: 1, start: 0, durationMs: 10, kind: 'partial', files: [], routes: [], pages: 1, ok: true, diagnostics: 0, ...p });
    expect(buildHeadline(rec({ files: ['src/components/LikeButton.tsx'] }))).toBe('<code>components/LikeButton.tsx</code> changed');
    expect(buildHeadline(rec({ ok: false, diagnostics: 3 }))).toBe('Build failed, 3 issues');
    expect(buildHeadline(rec({ kind: 'initial' }))).toBe('Initial build');
    expect(buildDetail(rec({ kind: 'css-only' }), [], '/')).toBe('Styles swapped in place, no reload');
    expect(buildDetail(rec({ routes: ['/blog/[slug]'] }), [], '/blog/[slug]')).toBe('1 page rebuilt, this tab reloaded');
    const failed = rec({ ok: false, start: 0 });
    expect(buildDetail(failed, [rec({ id: 2, start: 48000, files: ['src/pages/index.tsx'] })], '/')).toContain('Fixed 48s later in');
  });

  it('filters log lines', () => {
    const l = (src: string, lvl: LogLine['lvl'], text: string): LogLine => ({ seq: 1, t: 1, src, lvl, text });
    const lines = [l('krate', 'info', 'rebuilt'), l('api', 'error', 'TypeError'), l('browser', 'warn', 'console.warn img'), l('ssr', 'info', 'GET /dashboard')];
    expect(filterLogs(lines, 'server', '', false).map((x) => x.src)).toEqual(['krate', 'ssr']);
    expect(filterLogs(lines, 'api', '', false)).toHaveLength(1);
    expect(filterLogs(lines, 'browser', '', false)).toHaveLength(1);
    expect(filterLogs(lines, 'all', '', true).map((x) => x.text)).toEqual(['TypeError']);
    expect(filterLogs(lines, 'all', 'dash', false).map((x) => x.src)).toEqual(['ssr']);
  });
});
