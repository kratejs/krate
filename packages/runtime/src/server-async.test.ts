import { afterEach, describe, expect, it } from 'vitest';
import { cookies, headers } from './action-context.js';
import {
  getRequestContext,
  isNotFoundSignal,
  notFound,
  parseCookieHeader,
  redirectTarget,
  thenableRecord,
  type RequestContext,
} from './request-context.js';
import { Link, Suspense, jsx, renderToString, renderToStringAsync } from './server.js';

const REQUEST_KEY = Symbol.for('krate.request.store');

function withRequestStore(ctx: RequestContext) {
  (globalThis as Record<symbol, unknown>)[REQUEST_KEY] = { getStore: () => ctx };
}

afterEach(() => {
  delete (globalThis as Record<symbol, unknown>)[REQUEST_KEY];
});

const tick = () => new Promise((r) => setTimeout(r, 0));

describe('renderToStringAsync', () => {
  it('awaits an async page component', async () => {
    async function Page(props: { title: string }) {
      await tick();
      return jsx('h1', { children: props.title });
    }
    expect(await renderToStringAsync(jsx(Page, { title: 'Hi <you>' }))).toBe('<h1>Hi &lt;you&gt;</h1>');
  });

  it('awaits async children at any depth', async () => {
    async function Leaf(props: { n: number }) {
      await tick();
      return jsx('li', { children: String(props.n) });
    }
    function List() {
      return jsx('ul', { children: [jsx(Leaf, { n: 1 }), jsx(Leaf, { n: 2 })] });
    }
    async function Page() {
      await tick();
      return jsx('main', { children: jsx(List, {}) });
    }
    expect(await renderToStringAsync(jsx(Page, {}))).toBe('<main><ul><li>1</li><li>2</li></ul></main>');
  });

  it('resolves sibling async components in parallel', async () => {
    const order: string[] = [];
    let releaseA!: () => void;
    const gateA = new Promise<void>((r) => (releaseA = r));
    async function A() {
      await gateA;
      order.push('A');
      return 'a';
    }
    async function B() {
      order.push('B');
      releaseA();
      return 'b';
    }
    // B releases A's gate: a sequential renderer would deadlock waiting on A.
    const html = await renderToStringAsync(jsx('p', { children: [jsx(A, {}), jsx(B, {})] }));
    expect(html).toBe('<p>ab</p>');
    expect(order).toEqual(['B', 'A']);
  });

  it('renders the resolved children of <Suspense>, not the fallback', async () => {
    async function Slow() {
      await tick();
      return jsx('p', { children: 'done' });
    }
    const html = await renderToStringAsync(
      jsx(Suspense, { fallback: jsx('span', { children: 'loading' }), children: jsx(Slow, {}) }),
    );
    expect(html).toBe('<p>done</p>');
  });

  it('resolves async children passed to eager built-ins like <Link>', async () => {
    async function Label() {
      await tick();
      return 'Docs';
    }
    const html = await renderToStringAsync(jsx(Link, { href: '/docs', children: jsx(Label, {}) }));
    expect(html).toContain('>Docs</a>');
  });

  it('rejects when an async component throws', async () => {
    async function Boom() {
      await tick();
      throw new Error('db down');
    }
    await expect(renderToStringAsync(jsx('div', { children: jsx(Boom, {}) }))).rejects.toThrow('db down');
  });
});

describe('renderToString with async components', () => {
  it('throws instead of rendering an empty string', () => {
    async function Page() {
      return jsx('p', { children: 'x' });
    }
    expect(() => renderToString(jsx(Page, {}))).toThrow(/<Page> returned a Promise/);
  });
});

describe('request context', () => {
  const ctx: RequestContext = {
    url: '/p',
    method: 'GET',
    headers: new Headers({ 'x-test': 'yes', cookie: 'sid=abc; theme=dark' }),
    cookies: parseCookieHeader('sid=abc; theme=dark'),
    params: {},
    searchParams: {},
  };

  it('serves headers() and cookies() inside a server render, sync and awaited', async () => {
    withRequestStore(ctx);
    expect(getRequestContext()?.url).toBe('/p');
    expect(headers().get('x-test')).toBe('yes');
    expect((await headers()).get('x-test')).toBe('yes');
    const jar = cookies() as any;
    expect(jar.get('sid')).toEqual({ name: 'sid', value: 'abc' });
    expect((await (cookies() as any)).get('theme').value).toBe('dark');
    expect(jar.getAll()).toHaveLength(2);
  });

  it('refuses to modify cookies during a render', () => {
    withRequestStore(ctx);
    expect(() => (cookies() as any).set('a', 'b')).toThrow(/server action/);
  });

  it('throws outside any request or action scope', () => {
    expect(() => headers()).toThrow(/server component/);
  });
});

describe('thenable params', () => {
  it('supports property access and await', async () => {
    const params = thenableRecord({ slug: 'hello' });
    expect(params.slug).toBe('hello');
    const awaited = await params;
    expect(awaited.slug).toBe('hello');
    expect(JSON.stringify(params)).toBe('{"slug":"hello"}');
    expect(Object.keys(params)).toEqual(['slug']);
  });
});

describe('render signals', () => {
  it('notFound() throws a recognizable signal', () => {
    let caught: unknown;
    try {
      notFound();
    } catch (e) {
      caught = e;
    }
    expect(isNotFoundSignal(caught)).toBe(true);
    expect(isNotFoundSignal(new Error('other'))).toBe(false);
  });

  it('redirectTarget understands Krate and Next.js redirect errors', () => {
    expect(redirectTarget(Object.assign(new Error('x'), { __krateRedirect: '/a' }))).toBe('/a');
    expect(redirectTarget(new Error('NEXT_REDIRECT:/b'))).toBe('/b');
    expect(redirectTarget(Object.assign(new Error('NEXT_REDIRECT'), { digest: 'NEXT_REDIRECT;replace;/c;307;' }))).toBe('/c');
    expect(redirectTarget(new Error('nope'))).toBe('');
  });
});
