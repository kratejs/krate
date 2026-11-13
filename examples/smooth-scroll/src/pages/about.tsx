export default function About() {
  return (
    <>
      <Head>
        <title>About - Lenis smooth scroll</title>
      </Head>
      <section class="section">
        <h1>About</h1>
        <p>You should land at the top of this page after navigating from a scrolled position, with no jump back to the old position.</p>
        <div class="row">
          <Link class="button" href="/">Home</Link>
          <a class="button" href="#end">Jump to the end</a>
        </div>
      </section>
      <section class="section">
        <h2>Middle</h2>
        <p>Use the browser back button to return to the previous page. The saved position is restored through Lenis.</p>
      </section>
      <section class="section" id="end">
        <h2>End</h2>
        <p>The end of the page.</p>
      </section>
    </>
  );
}
