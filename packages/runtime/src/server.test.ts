import { describe, it, expect } from 'vitest';
import { jsx, renderToString } from './server';

describe('renderToString', () => {
  it('renders nested elements and escapes text', () => {
    const node = jsx('div', { class: 'x', children: [jsx('span', { children: 'a & b' })] });
    expect(renderToString(node)).toBe('<div class="x"><span>a &amp; b</span></div>');
  });

  it('drops event and internal props', () => {
    const node = jsx('button', { onClick: () => undefined, children: 'x' });
    expect(renderToString(node)).toBe('<button>x</button>');
  });

  it('blocks javascript: URLs and drops reserved props', () => {
    const html = renderToString(jsx('a', { href: 'javascript:alert(1)', ref: {}, key: 'k', children: 'x' }));
    expect(html).toContain('href="javascript:throw new Error(');
    expect(html).not.toContain(' ref=');
    expect(html).not.toContain(' key=');
  });
});

describe('server actions', () => {

  it('renders a formAction function as a POST submit endpoint', () => {
    const action = Object.assign(() => undefined, {
      actionUrl: '/__krate/action/a456',
    });
    const html = renderToString(jsx('button', { formAction: action, children: 'go' }));
    expect(html).toContain('formaction="/__krate/action/a456"');
    expect(html).toContain('formmethod="post"');
    expect(html).toContain('data-krate-action');
  });
});
