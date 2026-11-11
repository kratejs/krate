import { useParams } from '@krate/runtime';

export default function TaskPage() {
  const params = useParams();
  return (
    <main>
      <h1>Task {params.id}</h1>
      <a href="/">Back</a>
    </main>
  );
}
