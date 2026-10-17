import { describe, it, expect } from 'vitest';
import { reconcile } from './reconcile';

function markers(parent: HTMLElement): { start: Comment; end: Comment } {
  const start = document.createComment('s');
  const end = document.createComment('e');
  parent.append(start, end);
  return { start, end };
}

function texts(parent: HTMLElement, start: Node, end: Node): string[] {
  const out: string[] = [];
  let n = start.nextSibling;
  while (n && n !== end) {
    out.push((n as HTMLElement).textContent ?? '');
    n = n.nextSibling;
  }
  return out;
}

const span = (s: string): HTMLElement => {
  const el = document.createElement('span');
  el.textContent = s;
  return el;
};

describe('reconcile', () => {
  it('renders, shrinks, grows and replaces unkeyed lists', () => {
    const parent = document.createElement('div');
    const { start, end } = markers(parent);

    reconcile(parent, start, end, ['a', 'b', 'c'], span);
    expect(texts(parent, start, end)).toEqual(['a', 'b', 'c']);

    reconcile(parent, start, end, ['a', 'b'], span);
    expect(texts(parent, start, end)).toEqual(['a', 'b']);

    reconcile(parent, start, end, ['a', 'b', 'c', 'd'], span);
    expect(texts(parent, start, end)).toEqual(['a', 'b', 'c', 'd']);

    reconcile(parent, start, end, [], span);
    expect(texts(parent, start, end)).toEqual([]);
  });

  it('keyed reconciliation keeps order and drops removed keys', () => {
    const parent = document.createElement('div');
    const { start, end } = markers(parent);
    const map = (n: number) => span(String(n));

    reconcile(parent, start, end, [1, 2, 3], map, (n) => n);
    expect(texts(parent, start, end)).toEqual(['1', '2', '3']);

    reconcile(parent, start, end, [2, 3, 4], map, (n) => n);
    expect(texts(parent, start, end)).toEqual(['2', '3', '4']);

    reconcile(parent, start, end, [4, 2], map, (n) => n);
    expect(texts(parent, start, end)).toEqual(['4', '2']);
  });

  it('reuses existing DOM nodes when reordering keys (no node thrash)', () => {
    const parent = document.createElement('div');
    const { start, end } = markers(parent);
    let created = 0;
    const map = (n: number) => {
      created++;
      return span(String(n));
    };

    reconcile(parent, start, end, [1, 2, 3, 4, 5], map, (n) => n);
    expect(created).toBe(5);

    // Full reversal: every key still exists, so no node should be recreated.
    reconcile(parent, start, end, [5, 4, 3, 2, 1], map, (n) => n);
    expect(created).toBe(5);
    expect(texts(parent, start, end)).toEqual(['5', '4', '3', '2', '1']);
  });

  it('preserves the same node object for a persistent key', () => {
    const parent = document.createElement('div');
    const { start, end } = markers(parent);
    reconcile(parent, start, end, ['a', 'b'], span, (s) => s);
    const aNode = start.nextSibling; // the 'a' span

    reconcile(parent, start, end, ['x', 'a', 'b'], span, (s) => s); // prepend
    expect(texts(parent, start, end)).toEqual(['x', 'a', 'b']);
    expect(start.nextSibling!.nextSibling).toBe(aNode);
  });

  it('inserts a block of new keyed items in order', () => {
    const parent = document.createElement('div');
    const { start, end } = markers(parent);
    reconcile(parent, start, end, ['3', '4', '5'], span, (s) => s);
    reconcile(parent, start, end, ['1', '2', '3', '4', '5', '6'], span, (s) => s);
    expect(texts(parent, start, end)).toEqual(['1', '2', '3', '4', '5', '6']);
  });

  it('handles a large keyed reorder without thrash', () => {
    const parent = document.createElement('div');
    const { start, end } = markers(parent);
    const n = 1000;
    const range = Array.from({ length: n }, (_, i) => i);
    let created = 0;
    const map = (v: number) => {
      created++;
      return span(String(v));
    };

    reconcile(parent, start, end, range, map, (v) => v);
    expect(created).toBe(n);

    // Rotate by half: all keys persist, order changes.
    const rotated = range.slice(n / 2).concat(range.slice(0, n / 2));
    reconcile(parent, start, end, rotated, map, (v) => v);

    expect(created).toBe(n); // nothing recreated
    const got = texts(parent, start, end);
    expect(got.length).toBe(n);
    expect(got[0]).toBe(String(n / 2));
    expect(got[n - 1]).toBe(String(n / 2 - 1));
  });

  it('appends a large unkeyed tail in order', () => {
    const parent = document.createElement('div');
    const { start, end } = markers(parent);
    reconcile(parent, start, end, ['a', 'b'], span);
    const big = Array.from({ length: 500 }, (_, i) => 'v' + i);
    reconcile(parent, start, end, ['a', 'b'].concat(big), span);

    const got = texts(parent, start, end);
    expect(got.length).toBe(502);
    expect(got[0]).toBe('a');
    expect(got[501]).toBe('v499');
  });
});
