import { createResource, createSignal } from '@krate/runtime';

export default function ResourcePage() {
  const [id, setId] = createSignal(1);
  const [user, actions] = createResource(
    () => id(),
    async (n) => `user-${n}`,
  );

  return (
    <div class="page">
      <h1>Resource Demo</h1>
      <p>
        {user.loading ? 'Loading...' : user()}
      </p>
      <p>State: {user.state}</p>
      <button onClick={() => actions.refetch()}>Refetch</button>
      <button onClick={() => setId(id() + 1)}>Next</button>
    </div>
  )
}
