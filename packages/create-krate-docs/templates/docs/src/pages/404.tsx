export default function NotFound() {
  return (
    <div class="not-found">
      <Head>
        <title>404 — Page not found</title>
      </Head>
      <div class="not-found-code">404</div>
      <h1>Page not found</h1>
      <p>That page doesn't exist. Try the docs home page instead.</p>
      <a class="btn btn-primary" href="/docs/">Go to docs</a>
    </div>
  );
}
