<script>
  const items = Array.from({ length: 100 }, (_, i) => i);
</script>

<main>
  <h1>Benchmark</h1>
  <p>Static SvelteKit page</p>
  <ul>
    {#each items as i (i)}
      <li>Item {i}</li>
    {/each}
  </ul>
</main>
