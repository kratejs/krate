<script>
  let count = $state(0);
  const items = Array.from({ length: 100 }, (_, i) => i);
</script>

<main>
  <h1>Benchmark</h1>
  <p>Count: {count}</p>
  <button onclick={() => count++}>+</button>
  <ul>
    {#each items as i (i)}
      <li>Item {i}</li>
    {/each}
  </ul>
</main>
