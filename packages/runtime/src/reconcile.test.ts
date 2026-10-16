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
});
