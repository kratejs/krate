import { describe, it, expect, beforeEach } from 'vitest';
import { createSignal, createEffect, disposeAll, krateHookState } from './signal';
import './effects';
import { __krateOptimistic, runActionTransition } from './actions';

const tick = () => new Promise((r) => setTimeout(r, 0));
async function settle(): Promise<void> {
  for (let i = 0; i < 6; i++) await tick();
}

type Msg = { text: string; sending?: boolean };

describe('state written at the end of an async action', () => {
  beforeEach(() => disposeAll());

  // The update cascades through several effects (optimistic state, a list
  // binding, row bindings). The action settles, and its optimistic revert
  // reads the committed state, in a microtask that used to run between two
  // passes of that cascade, before the state was committed.
  for (const hops of [0, 1, 2, 3, 4]) it('survives an unrelated plain signal write in a later microtask (' + hops + ' hops)', async () => {
    const [messages, setMessages] = createSignal<Msg[]>([{ text: 'hi' }], krateHookState);
    const [optimistic, setOptimistic] = createSignal<Msg[]>(messages());
    const add = __krateOptimistic<Msg[], string>(
      () => messages(),
      setOptimistic,
      (s, text) => [...s, { text, sending: true }],
    );
    const [rows, setRows] = createSignal<string[]>([]);
    createEffect(() => setRows(optimistic().map((m) => m.text + (m.sending ? '*' : ''))));
    const [label, setLabel] = createSignal('');
    createEffect(() => setLabel(rows().join(',')));
    const shown: string[] = [];
    createEffect(() => void shown.push(label()));
    await settle();
    void runActionTransition(async () => {
      add('hello');
      await new Promise((r) => setTimeout(r, 5));
      // A startTransition that writes its own pending signal afterwards.
      const s = createSignal(true);
      setMessages((m) => [...m, { text: 'hello' }]);
      queueMicrotask(() => s[1](false));
      // The action resolves a few microtasks later (the form action wrapper).
      for (let i = 0; i < hops; i++) await null;
    });
    await settle();
    await new Promise((r) => setTimeout(r, 20));
    expect(messages().map((m) => m.text)).toEqual(['hi', 'hello']);
    expect(optimistic().map((m) => m.text)).toEqual(['hi', 'hello']);
    expect(shown[shown.length - 1]).toBe('hi,hello');
  });
});
