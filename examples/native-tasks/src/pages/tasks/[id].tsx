import { createSignal, createEffect, useParams } from '@krate/runtime';
import { Get } from '../../lib/tasks.go';

export default function TaskPage() {
  const params = useParams();
  const [title, setTitle] = createSignal('');
  const [error, setError] = createSignal('');

  createEffect(async () => {
    try {
      const t = await Get(Number(params.id));
      setTitle(t.title);
      setError('');
    } catch (e) {
      setTitle('');
      setError(e.code);
    }
  });

  return (
    <main>
      <h1 id="heading">Task {params.id}</h1>
      <p id="title">{title()}</p>
      <p id="error">{error()}</p>
      <Link id="back" href="/">Back</Link>
    </main>
  );
}
