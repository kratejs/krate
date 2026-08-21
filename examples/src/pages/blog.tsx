import { createSignal } from '@krate/runtime';
import styles from './Card.module.css';

function Counter(props) {
  const [count, setCount] = createSignal(props.initial);
  return (
    <div class="counter">
      <span class="count">{count()}</span>
      <button onClick={() => setCount(c => c + 1)}>+</button>
      {count() > 0 && (
        <button onClick={() => setCount(c => c - 1)}>-</button>
      )}
    </div>
  );
}

export default function Blog() {
  const title = "Getting Started with Krate";
  const date = "July 5, 2026";
  const author = "Krate Team";
  return (
    <>
      <Head>
        <title>{title} - Krate Blog</title>
        <meta name="description" content="Learn how to build web apps with Krate" />
      </Head>
      <div class={styles.card}>
        <span class={styles.tag}>Announcement</span>
        <h1>{title}</h1>
        <p class="meta">By {author} - {date}</p>
        <p>Krate makes it easy to build fast, interactive web applications.</p>
      </div>
      <div class="card">
        <h2>Interactive Counter</h2>
        <p>This counter is hydrated on the client side using signals:</p>
        <Counter initial={0} />
      </div>
      <div class="card">
        <h2>Why Krate?</h2>
        <ul>
          <li>Zero JavaScript runtime overhead for static content</li>
          <li>Fine-grained reactivity only where you need it</li>
          <li>Tailwind ships only the utility classes you actually use</li>
          <li>File-based routing and layouts for clean project structure</li>
        </ul>
      </div>
    </>
  );
}
