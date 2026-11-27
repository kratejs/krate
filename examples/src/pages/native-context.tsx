import { createContext, createSignal, createResource } from '@krate/runtime';

// Native context values: objects that carry getters and setters, and a
// primitive read in string, template, attribute and conditional positions.
const CounterCtx = createContext({ count: () => 0, setCount: (_n: number) => {}, step: 1 });
const DensityCtx = createContext('compact');

function useCounter() {
  const [count, setCount] = createSignal(0);
  return { count, setCount, step: 5 };
}

function Counter() {
  const { count, setCount } = CounterCtx.useContext();
  return (
    <button data-testid="ctx-destructured" onClick={() => setCount(count() + 1)}>
      {count()}
    </button>
  );
}

function Stepper() {
  const store = CounterCtx.useContext();
  return (
    <button data-testid="ctx-member" onClick={() => store.setCount(store.count() + store.step)}>
      {`n=${store.count()}`}
    </button>
  );
}

function Row() {
  const density = DensityCtx.useContext();
  return (
    <div data-testid="ctx-row" class={'cx-' + density} data-info={`d=${density}`}>
      {density === 'wide' ? 'Wide' : 'Compact'}
    </div>
  );
}

export default function NativeContextPage() {
  const store = useCounter();
  const [density, setDensity] = createSignal('compact');

  // Derived values, written as local functions.
  const [items, setItems] = createSignal([{ price: 2 }, { price: 3 }]);
  const total = () => items().reduce((sum, item) => sum + item.price, 0);
  function count() {
    return items().length;
  }

  // A bare getter is the reactive source of a resource.
  const [id, setId] = createSignal('a');
  const [byGetter] = createResource(id, (key: string) => new Promise<string>((resolve) => setTimeout(() => resolve('got-' + key), 10)));

  return (
    <div class="page">
      <CounterCtx.Provider value={store}>
        <Counter />
        <Stepper />
      </CounterCtx.Provider>
      <DensityCtx.Provider value={density()}>
        <button data-testid="ctx-toggle" onClick={() => setDensity(density() === 'compact' ? 'wide' : 'compact')}>
          toggle
        </button>
        <Row />
      </DensityCtx.Provider>
      <b data-testid="derived-total">{total()}</b>
      <i data-testid="derived-count">{count()}</i>
      <button data-testid="derived-add" onClick={() => setItems([...items(), { price: 5 }])}>
        add
      </button>
      <p data-testid="resource-value">{byGetter()}</p>
      <button data-testid="resource-next" onClick={() => setId(id() + 'x')}>
        next
      </button>
    </div>
  );
}
