export default function Home() {
  const [count, setCount] = createSignal(0);
  return (
    <>
      <Head>
        <title>Lenis smooth scroll</title>
      </Head>
      <section class="section" id="top" data-lenis-snap="start">
        <h1>Smooth scrolling with Lenis</h1>
        <p>Scroll with the mouse wheel or trackpad. The readout at the bottom right shows the live Lenis scroll position and velocity.</p>
        <div class="row">
          <a class="button" href="#anchors">Jump to anchors</a>
          <a class="button" href="#nested">Jump to nested scroll</a>
          <button class="button" onClick={() => setCount(count() + 1)}>Clicked {count()}</button>
        </div>
      </section>
      <section class="section" id="anchors" data-lenis-snap="start">
        <h2>Anchors</h2>
        <p>Links to in-page ids animate here, offset by the sticky navbar height set in the config.</p>
        <div class="row">
          <a class="button" href="#top">Back to top</a>
          <a class="button" href="#snap">Jump to snap</a>
        </div>
      </section>
      <section class="section" id="nested">
        <h2>Nested scrolling</h2>
        <p>The first box carries data-lenis-prevent, so it scrolls natively. The second box matches the prevent selector from the config.</p>
        <div class="scrollbox" data-lenis-prevent>
          <p>Native scroll box. Use the wheel here and the page behind stays put.</p>
          <p>Line two of the native box.</p>
          <p>Line three of the native box.</p>
          <p>Line four of the native box.</p>
          <p>Line five of the native box.</p>
          <p>Line six of the native box.</p>
        </div>
        <div class="scrollbox no-smooth">
          <p>Selector prevent box. Matches the .no-smooth selector.</p>
          <p>Line two of the selector box.</p>
          <p>Line three of the selector box.</p>
          <p>Line four of the selector box.</p>
          <p>Line five of the selector box.</p>
          <p>Line six of the selector box.</p>
        </div>
      </section>
      <section class="section" id="snap" data-lenis-snap="start">
        <h2>Snap point one</h2>
        <p>With proximity snap, releasing the wheel close to a snap point settles on it. Sections marked data-lenis-snap are the snap points.</p>
      </section>
      <section class="section" data-lenis-snap="start">
        <h2>Snap point two</h2>
        <p>Keep scrolling slowly and stop near the top of this section.</p>
      </section>
      <section class="section" data-lenis-snap="start">
        <h2>Snap point three</h2>
        <p>Open the About page to check that client-side navigation resets scroll through Lenis.</p>
        <div class="row">
          <Link class="button" href="/about">Go to About</Link>
        </div>
      </section>
    </>
  );
}
