export default function About() {
  return (
    <section class="hero">
      <Head>
        <title>About — __PROJECT_DISPLAY_NAME__</title>
      </Head>
      <h1 class="hero-title">About</h1>
      <p class="hero-subtitle">
        This is a static page, defined as{" "}
        <code>src/pages/about.tsx</code> and reachable at{" "}
        <code>/about</code>. Krate maps files under <code>src/pages/</code> to
        URLs automatically.
      </p>
      <div class="hero-actions">
        <a class="btn btn-primary" href="/">Back home</a>
      </div>
    </section>
  );
}
