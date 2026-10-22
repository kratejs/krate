import { describe, it, expect, vi, afterEach } from 'vitest';
import { createSignal, onMount, onCleanup } from './signal';
import { h } from './dom';
import { krateList, krateBindList, longestIncreasingSubsequence } from './list';

type Item = { id: number; label: string };

const tick = () => new Promise<void>((r) => setTimeout(r, 0));

function mountList(node: Node): HTMLElement {
  const root = document.createElement('div');
  document.body.appendChild(root);
  root.appendChild(node);
  return root;
}

function rowTexts(root: Element, selector = 'li'): string[] {
  return Array.from(root.querySelectorAll(selector)).map((el) => el.textContent || '');
}

afterEach(() => {
  document.body.innerHTML = '';
});

describe('longestIncreasingSubsequence', () => {
  it('ignores negative entries and returns indices in order', () => {
    expect(longestIncreasingSubsequence([2, 0, 1, -1, 3])).toEqual([1, 2, 4]);
    expect(longestIncreasingSubsequence([3, 2, 1, 0])).toHaveLength(1);
    expect(longestIncreasingSubsequence([])).toEqual([]);
  });
});

describe('keyed lists', () => {
  it('reorders by key keeping DOM nodes, input values and focus', async () => {
    const [items, setItems] = createSignal<Item[]>([
      { id: 1, label: 'a' },
      { id: 2, label: 'b' },
      { id: 3, label: 'c' },
    ]);
    const root = mountList(
      krateList(
        () => items(),
        (t: any) => t.id,
        (t: any) => h('li', null, () => t().label, h('input', { class: () => 'in-' + t().id })),
        1,
      ).mount(),
    );
    const inputs = Array.from(root.querySelectorAll('input'));
    inputs[0].value = 'typed-a';
    inputs[2].focus();
    const lis = Array.from(root.querySelectorAll('li'));

    setItems([...items()].reverse());
    await tick();

    expect(rowTexts(root)).toEqual(['c', 'b', 'a']);
    const after = Array.from(root.querySelectorAll('li'));
    expect(after).toEqual([lis[2], lis[1], lis[0]]);
    expect((root.querySelector('.in-1') as HTMLInputElement).value).toBe('typed-a');
    expect(document.activeElement).toBe(inputs[2]);
  });

  it('keeps component state per key and updates item values in place', async () => {
    const [items, setItems] = createSignal<Item[]>([
      { id: 1, label: 'one' },
      { id: 2, label: 'two' },
    ]);
    function Row(props: any) {
      const [clicks, setClicks] = createSignal(0);
      return h(
        'li',
        null,
        h('span', { class: 'label' }, () => props.item.label),
        h('button', { onClick: () => setClicks(clicks() + 1) }, () => clicks()),
        h('em', null, () => props.index),
      );
    }
    const root = mountList(
      krateList(
        () => items(),
        (t: any) => t.id,
        (t: any, i: any) => h(Row, {
          get item() {
            return t();
          },
          get index() {
            return i();
          },
        }),
        1,
      ).mount(),
    );
    (root.querySelectorAll('button')[1] as HTMLButtonElement).click();
    (root.querySelectorAll('button')[1] as HTMLButtonElement).click();
    await tick();
    expect(root.querySelectorAll('button')[1].textContent).toBe('2');

    // New object for key 2 and a move to the front: state stays, label and index update.
    setItems([{ id: 2, label: 'TWO' }, { id: 1, label: 'one' }]);
    await tick();
    expect(rowTexts(root, '.label')).toEqual(['TWO', 'one']);
    expect(root.querySelectorAll('button')[0].textContent).toBe('2');
    expect(rowTexts(root, 'em')).toEqual(['0', '1']);
  });

  it('runs cleanups for removed rows before new rows mount', async () => {
    const log: string[] = [];
    const [items, setItems] = createSignal<number[]>([1, 2, 3]);
    function Row(props: any) {
      const id = props.id;
      onMount(() => {
        log.push('mount ' + id);
        onCleanup(() => log.push('unmount ' + id));
      });
      return h('li', null, String(id));
    }
    const root = mountList(
      krateList(
        () => items(),
        (n: any) => n,
        (n: any) => h(Row, { get id() { return n(); } }),
        1,
      ).mount(),
    );
    await tick();
    expect(log).toEqual(['mount 1', 'mount 2', 'mount 3']);
    log.length = 0;

    setItems([1, 4, 3]);
    await tick();
    expect(rowTexts(root)).toEqual(['1', '4', '3']);
    expect(log).toEqual(['unmount 2', 'mount 4']);
  });

  it('inserts and removes in the middle with minimal DOM work', async () => {
    const [items, setItems] = createSignal<number[]>([1, 2, 3, 4]);
    const root = mountList(krateList(() => items(), (n: any) => n, (n: any) => h('li', null, () => String(n())), 1).mount());
    const before = Array.from(root.querySelectorAll('li'));
    setItems([1, 2, 9, 3, 4]);
    await tick();
    expect(rowTexts(root)).toEqual(['1', '2', '9', '3', '4']);
    const after = Array.from(root.querySelectorAll('li'));
    expect(after[0]).toBe(before[0]);
    expect(after[3]).toBe(before[2]);
    setItems([1, 9, 4]);
    await tick();
    expect(rowTexts(root)).toEqual(['1', '9', '4']);
    expect(root.querySelectorAll('li')[2]).toBe(before[3]);
  });

  it('moves only rows outside the longest increasing run', async () => {
    const [items, setItems] = createSignal<number[]>([1, 2, 3, 4, 5]);
    const root = mountList(krateList(() => items(), (n: any) => n, (n: any) => h('li', null, String(n())), 1).mount());
    const ul = root;
    const spy = vi.spyOn(ul, 'insertBefore');
    setItems([5, 1, 2, 3, 4]);
    await tick();
    expect(rowTexts(root)).toEqual(['5', '1', '2', '3', '4']);
    // One row moved: its start marker, element and end marker.
    expect(spy.mock.calls.length).toBe(3);
    spy.mockRestore();
  });

  it('warns on duplicate keys and keeps the first occurrence on its row', async () => {
    const err = vi.spyOn(console, 'error').mockImplementation(() => {});
    const [items, setItems] = createSignal<Item[]>([
      { id: 1, label: 'a' },
      { id: 1, label: 'b' },
    ]);
    const root = mountList(krateList(() => items(), (t: any) => t.id, (t: any) => h('li', null, () => t().label), 1).mount());
    expect(rowTexts(root)).toEqual(['a', 'b']);
    expect(err).toHaveBeenCalled();
    expect(String(err.mock.calls[0][0])).toContain('same key');
    const first = root.querySelector('li');
    setItems([{ id: 1, label: 'a2' }, { id: 1, label: 'b2' }, { id: 2, label: 'c' }]);
    await tick();
    expect(rowTexts(root)).toEqual(['a2', 'b2', 'c']);
    expect(root.querySelector('li')).toBe(first);
    err.mockRestore();
  });

  it('compares keys as strings like React', async () => {
    const [items, setItems] = createSignal<Array<{ k: string | number }>>([{ k: 1 }, { k: 2 }]);
    const root = mountList(krateList(() => items(), (t: any) => t.k, () => h('li', null, h('input', null)), 1).mount());
    root.querySelectorAll('input')[0].value = 'kept';
    setItems([{ k: '2' }, { k: '1' }]);
    await tick();
    expect(root.querySelectorAll('input')[1].value).toBe('kept');
  });

  it('remounts a keyed single element when its key changes', async () => {
    const log: string[] = [];
    const [user, setUser] = createSignal('ada');
    function Profile(props: any) {
      const [draft, setDraft] = createSignal('');
      const name = props.name;
      onMount(() => {
        log.push('mount ' + name);
        onCleanup(() => log.push('unmount ' + name));
      });
      return h('input', { value: () => draft(), onInput: (e: any) => setDraft(e.target.value), 'data-user': name });
    }
    const root = mountList(
      krateList(() => [user()], (k: any) => k, () => h(Profile, { get name() { return user(); } }), 1).mount(),
    );
    await tick();
    const input = root.querySelector('input') as HTMLInputElement;
    input.value = 'draft';
    input.dispatchEvent(new Event('input'));
    setUser('ada');
    await tick();
    expect(root.querySelector('input')).toBe(input);

    setUser('grace');
    await tick();
    const next = root.querySelector('input') as HTMLInputElement;
    expect(next).not.toBe(input);
    expect(next.value).toBe('');
    expect(next.getAttribute('data-user')).toBe('grace');
    expect(log).toEqual(['mount ada', 'unmount ada', 'mount grace']);
  });

  it('renders keyed fragments, text rows and nested lists', async () => {
    const [groups, setGroups] = createSignal([
      { id: 'g1', items: ['x', 'y'] },
      { id: 'g2', items: ['z'] },
    ]);
    const root = mountList(
      krateList(
        () => groups(),
        (g: any) => g.id,
        (g: any) => [
          h('dt', null, () => g().id),
          h('dd', null, krateList(() => g().items, null, (s: any) => () => s(), 1)),
        ],
        1,
      ).mount(),
    );
    expect(rowTexts(root, 'dt')).toEqual(['g1', 'g2']);
    expect(rowTexts(root, 'dd')).toEqual(['xy', 'z']);
    const dts = Array.from(root.querySelectorAll('dt'));
    setGroups([{ id: 'g2', items: ['z', 'w'] }, { id: 'g1', items: ['y'] }]);
    await tick();
    expect(rowTexts(root, 'dt')).toEqual(['g2', 'g1']);
    expect(rowTexts(root, 'dd')).toEqual(['zw', 'y']);
    expect(Array.from(root.querySelectorAll('dt'))).toEqual([dts[1], dts[0]]);
  });

  it('switches between empty and non-empty and works inside tables', async () => {
    const [rows, setRows] = createSignal<number[]>([]);
    const table = document.createElement('table');
    const tbody = document.createElement('tbody');
    table.appendChild(tbody);
    document.body.appendChild(table);
    tbody.appendChild(krateList(() => rows(), (n: any) => n, (n: any) => h('tr', null, h('td', null, () => String(n()))), 1).mount());
    expect(tbody.querySelectorAll('tr')).toHaveLength(0);
    setRows([1, 2]);
    await tick();
    expect(rowTexts(tbody, 'td')).toEqual(['1', '2']);
    setRows([]);
    await tick();
    expect(tbody.querySelectorAll('tr')).toHaveLength(0);
    expect(Array.from(tbody.childNodes).every((n) => n.nodeType === 8)).toBe(true);
  });

  it('uses index keys for unkeyed lists (state stays with the position)', async () => {
    const [items, setItems] = createSignal(['a', 'b']);
    const root = mountList(krateList(() => items(), null, (s: any) => h('li', null, () => s(), h('input', null)), 1).mount());
    root.querySelectorAll('input')[0].value = 'first';
    setItems(['b', 'a']);
    await tick();
    expect(rowTexts(root)).toEqual(['b', 'a']);
    expect(root.querySelectorAll('input')[0].value).toBe('first');
  });

  it('value mode re-renders a row whose item changed', async () => {
    const [items, setItems] = createSignal<Item[]>([{ id: 1, label: 'a' }]);
    const root = mountList(krateList(() => items(), (t: any) => t.id, (t: any) => h('li', null, t.label), 0).mount());
    const li = root.querySelector('li');
    setItems([{ id: 1, label: 'b' }]);
    await tick();
    expect(rowTexts(root)).toEqual(['b']);
    expect(root.querySelector('li')).not.toBe(li);
  });

  it('detaches refs of removed rows', async () => {
    const refs: Array<Element | null> = [];
    const [items, setItems] = createSignal([1]);
    mountList(krateList(() => items(), (n: any) => n, () => h('li', { ref: (el: Element | null) => refs.push(el) }), 1).mount());
    setItems([]);
    await tick();
    expect(refs).toHaveLength(2);
    expect(refs[1]).toBeNull();
  });
});

describe('hydration adoption', () => {
  it('adopts server-rendered rows by key without replacing nodes', async () => {
    const root = document.createElement('ul');
    root.innerHTML =
      '<!--k:L--><!--k:L:$1--><li data-id="1">one<button>0</button></li><!--/k:L:$1-->' +
      '<!--k:L:$2--><li data-id="2">two<button>0</button></li><!--/k:L:$2--><!--/k:L-->';
    document.body.appendChild(root);
    const ssr = Array.from(root.querySelectorAll('li'));
    const [items, setItems] = createSignal<Item[]>([
      { id: 1, label: 'one' },
      { id: 2, label: 'two' },
    ]);
    function Row(props: any) {
      const [n, setN] = createSignal(0);
      return h('li', { 'data-id': () => props.item.id }, () => props.item.label, h('button', { onClick: () => setN(n() + 1) }, () => n()));
    }
    krateBindList(
      root.firstChild as Comment,
      () => items(),
      (t: any) => t.id,
      (t: any) => h(Row, { get item() { return t(); } }),
      1,
    );
    expect(Array.from(root.querySelectorAll('li'))).toEqual(ssr);
    (root.querySelectorAll('button')[1] as HTMLButtonElement).click();
    await tick();
    expect(root.querySelectorAll('button')[1].textContent).toBe('1');

    setItems([{ id: 2, label: 'two' }, { id: 1, label: 'uno' }]);
    await tick();
    expect(Array.from(root.querySelectorAll('li'))).toEqual([ssr[1], ssr[0]]);
    expect(rowTexts(root)).toEqual(['two1', 'uno0']);
  });

  it('adopts by position when the build could not compute keys and patches mismatches', async () => {
    const root = document.createElement('ul');
    root.innerHTML = '<!--k:M--><!--k:M:0--><li>stale</li><!--/k:M:0--><!--k:M:1--><li>gone</li><!--/k:M:1--><!--/k:M-->';
    document.body.appendChild(root);
    const first = root.querySelector('li');
    const [items] = createSignal([{ id: 'a', label: 'fresh' }]);
    krateBindList(root.firstChild as Comment, () => items(), (t: any) => t.id, (t: any) => h('li', null, () => t().label), 1);
    expect(root.querySelectorAll('li')).toHaveLength(1);
    expect(root.querySelector('li')).toBe(first);
    expect(rowTexts(root)).toEqual(['fresh']);
  });

  it('adopts nested server-rendered lists inside rows', () => {
    const root = document.createElement('div');
    root.innerHTML =
      '<!--k:O--><!--k:O:$a--><section><h2>a</h2><ul><!--k:P--><!--k:P:$x--><li>x</li><!--/k:P:$x--><!--/k:P--></ul></section><!--/k:O:$a--><!--/k:O-->';
    document.body.appendChild(root);
    const li = root.querySelector('li');
    const [groups] = createSignal([{ id: 'a', items: ['x'] }]);
    krateBindList(
      root.firstChild as Comment,
      () => groups(),
      (g: any) => g.id,
      (g: any) => h('section', null, h('h2', null, () => g().id), h('ul', null, krateList(() => g().items, (s: any) => s, (s: any) => h('li', null, () => s()), 1))),
      1,
    );
    expect(root.querySelector('li')).toBe(li);
    expect(root.querySelectorAll('li')).toHaveLength(1);
  });
});

describe('list performance', () => {
  it('reorders, inserts and removes 1000 rows in roughly linear time', async () => {
    const make = (n: number) => Array.from({ length: n }, (_, i) => ({ id: i, label: 'row ' + i }));
    const [items, setItems] = createSignal(make(1000));
    const root = mountList(
      krateList(() => items(), (t: any) => t.id, (t: any) => h('li', null, () => t().label), 1).mount(),
    );
    expect(root.querySelectorAll('li')).toHaveLength(1000);

    const time = async (next: Item[]) => {
      const t0 = performance.now();
      setItems(next);
      await tick();
      return performance.now() - t0;
    };
    const reversed = await time([...items()].reverse());
    expect(root.querySelector('li')!.textContent).toBe('row 999');
    const shuffled = [...items()];
    for (let i = shuffled.length - 1; i > 0; i--) {
      const j = (i * 7919) % (i + 1);
      [shuffled[i], shuffled[j]] = [shuffled[j], shuffled[i]];
    }
    const shuffle = await time(shuffled);
    const inserted = [...items()];
    inserted.splice(500, 0, { id: 5000, label: 'inserted' });
    const insert = await time(inserted);
    const removed = items().filter((_, i) => i % 2 === 0);
    const remove = await time(removed);
    expect(root.querySelectorAll('li')).toHaveLength(removed.length);

    // Generous ceilings: happy-dom is slow, but a quadratic algorithm over
    // 1000 rows takes far longer than this.
    for (const ms of [reversed, shuffle, insert, remove]) expect(ms).toBeLessThan(2000);
  });
});
