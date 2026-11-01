export default function NotFound() {
  return (
    <div class="not-found">
      <Head>
        <title>404 - Page not found - Krate</title>
        <meta name="description" content="The page you requested could not be found. Browse the Krate documentation home instead." />
        <meta name="robots" content="noindex" />
      </Head>
      <div class="not-found-code">404</div>
      <h1>Page not found</h1>
      <p>That page doesn't exist. Try the docs home page instead.</p>
      <a class="btn btn-primary" href="/docs/">Go to docs</a>
    </div>
  );
}
