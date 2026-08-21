import { createSignal } from '@krate/runtime';

interface GreetingProps {
    name: string;
}

function Greeting(props: GreetingProps) {
  return <h1>Hello, {props.name}!</h1>;
}

interface CounterProps {
    initial: number;
}

function Counter(props: CounterProps) {
  const [count, setCount] = createSignal(props.initial);

  return (
    <div class="counter">
      <span class="count">{count()}</span>
      <button onClick={() => setCount((c: number) => c + 1)}>+</button>
      {count() > 0 && (
        <button onClick={() => setCount((c: number) => c - 1)}>-</button>
      )}
    </div>
  );
}

function App() {
  const [name, setName] = createSignal('World');

  return (
    <>
      <div class="card p-6 m-4 bg-white rounded-xl shadow-lg">
        <h1 class="text-2xl font-bold text-gray-800">Krate Framework</h1>
        <p class="mt-2 text-gray-600">A fast, lightweight web framework with a Go compiler. JSX/TSX pages compile to static HTML with client-side hydration.</p>
        <div class="flex flex-wrap gap-4 mt-4">
          <div class="flex-1 p-3 bg-blue-50 rounded-lg"><strong>Signals</strong><br/>Fine-grained reactivity</div>
          <div class="flex-1 p-3 bg-blue-50 rounded-lg"><strong>SSR</strong><br/>Server-rendered HTML</div>
          <div class="flex-1 p-3 bg-blue-50 rounded-lg"><strong>Hydration</strong><br/>Interactive UI</div>
          <div class="flex-1 p-3 bg-blue-50 rounded-lg"><strong>CSS</strong><br/>Modules & Tailwind</div>
        </div>
      </div>

      <div class="card">
        <h2>Interactive Greeting</h2>
        <Greeting name={name()} />
        <input
          value={name()}
          onInput={(e) => setName(e.target.value)}
          placeholder="Enter your name"
        />
      </div>

      <div class="card">
        <h2>Counters</h2>
        <Counter initial={0} />
        <Counter initial={100} />
      </div>

      <div class="card">
        <h2>Fruits</h2>
        <ul>
          {['Apple', 'Banana', 'Cherry', 'Date', 'Elderberry'].map(item => (
            <li>{item}</li>
          ))}
        </ul>
      </div>

      <div class="card">
        <h2>Responsive Image</h2>
        <Image
          src="/hero.png"
          width={800}
          alt="Responsive demo image"
          priority
        />
      </div>
    </>
  );
}

export default App;
