import {
  Suspense,
  lazy,
  use,
  useState,
  useTransition,
  useDeferredValue,
  useSyncExternalStore,
} from 'react';

// Conformance corpus for React Suspense, lazy, use() and transitions. The
// browser conformance test drives every section (see browser_conformance.mjs).

const LazyPanel = lazy(() => import('../components/LazyPanel'));

function later(value: string, ms: number): Promise<string> {
  return new Promise((resolve) => setTimeout(() => resolve(value), ms));
}

const greeting = later('Hello from use()', 700);
const fast = later('fast', 300);
const slow = later('slow', 900);
const nested = later('nested content', 900);

function Message(props: { source: Promise<string>; testid: string }) {
  const text = use(props.source);
  return <span data-testid={props.testid}>{text}</span>;
}

// A tiny external store for useSyncExternalStore.
let storeValue = 0;
const listeners = new Set<() => void>();
function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}
function bump() {
  storeValue += 1;
  listeners.forEach((l) => l());
}

function StoreReader() {
  const value = useSyncExternalStore(subscribe, () => storeValue, () => 0);
  return (
    <p class="flex items-center gap-3">
      <button type="button" data-testid="store-bump" onClick={() => bump()}>
        bump store
      </button>
      <span data-testid="store-value">{value}</span>
    </p>
  );
}

function DeferredSearch() {
  const [query, setQuery] = useState('');
  const deferred = useDeferredValue(query);
  return (
    <p class="flex items-center gap-3">
      <input
        data-testid="deferred-input"
        value={query}
        placeholder="type to defer"
        onInput={(event: any) => setQuery(event.target.value)}
      />
      <span data-testid="deferred-value">{deferred}</span>
    </p>
  );
}

export default function Page() {
  const [source, setSource] = useState(later('first', 400));
  const [isPending, startTransition] = useTransition();
  const [actionPending, startAction] = useTransition();
  const [saved, setSaved] = useState('not saved');

  return (
    <div class="mx-auto max-w-2xl space-y-6 px-6 py-10">
      <header class="space-y-2">
        <p class="text-xs font-semibold uppercase tracking-wide text-indigo-300">Conformance corpus</p>
        <h1 class="text-3xl font-bold text-slate-100">React Suspense and transitions</h1>
      </header>

      <section class="space-y-2">
        <h2>use(promise)</h2>
        <Suspense fallback={<span data-testid="use-fallback">Loading greeting...</span>}>
          <Message source={greeting} testid="use-content" />
        </Suspense>
      </section>

      <section class="space-y-2">
        <h2>Siblings reveal together</h2>
        <Suspense fallback={<span data-testid="siblings-fallback">Loading both...</span>}>
          <Message source={fast} testid="sibling-fast" />
          <Message source={slow} testid="sibling-slow" />
        </Suspense>
      </section>

      <section class="space-y-2">
        <h2>Nested boundaries</h2>
        <Suspense fallback={<span data-testid="outer-fallback">Loading outer...</span>}>
          <p data-testid="outer-shell">outer shell</p>
          <Suspense fallback={<span data-testid="inner-fallback">Loading inner...</span>}>
            <Message source={nested} testid="inner-content" />
          </Suspense>
        </Suspense>
      </section>

      <section class="space-y-2">
        <h2>lazy()</h2>
        <Suspense fallback={<span>Loading panel...</span>}>
          <LazyPanel title="Lazy panel" />
        </Suspense>
      </section>

      <section class="space-y-2">
        <h2>Transitions keep revealed content</h2>
        <p class="flex gap-3">
          <button type="button" data-testid="source-urgent" onClick={() => setSource(later('urgent', 500))}>
            urgent update
          </button>
          <button
            type="button"
            data-testid="source-transition"
            onClick={() => startTransition(() => setSource(later('transition', 500)))}
          >
            transition update
          </button>
          <span data-testid="transition-pending">{isPending ? 'pending' : 'idle'}</span>
        </p>
        <Suspense fallback={<span data-testid="source-fallback">Loading source...</span>}>
          <Message source={source} testid="source-content" />
        </Suspense>
      </section>

      <section class="space-y-2">
        <h2>Async actions</h2>
        <p class="flex gap-3">
          <button
            type="button"
            data-testid="action-start"
            onClick={() =>
              startAction(async () => {
                await later('', 300);
                setSaved('saved');
              })
            }
          >
            save
          </button>
          <span data-testid="action-pending">{actionPending ? 'pending' : 'idle'}</span>
          <span data-testid="action-result">{saved}</span>
        </p>
      </section>

      <section class="space-y-2">
        <h2>useDeferredValue and useSyncExternalStore</h2>
        <DeferredSearch />
        <StoreReader />
      </section>
    </div>
  );
}
