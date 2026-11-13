function MotionDemo() {
  return (
    <>
      <Head>
        <title>Motion demo</title>
        <meta name="description" content="Motion animations driven by data-motion attributes" />
      </Head>
      <div class="card" data-motion="fade-up" data-motion-trigger="load">
        <h1>Motion</h1>
        <p>Every block on this page is animated by data-motion attributes and the motion option in krate.config.ts.</p>
      </div>

      <div class="card" data-motion-stagger="0.12">
        <h2>Staggered presets</h2>
        <p data-motion="fade-up">fade-up</p>
        <p data-motion="fade-left">fade-left</p>
        <p data-motion="zoom-in">zoom-in</p>
        <p data-motion="blur-in">blur-in</p>
        <p data-motion="pop">pop, a custom preset from the config</p>
      </div>

      <div class="card">
        <h2>Gestures</h2>
        <button data-motion-hover='{"scale":1.08}' data-motion-press='{"scale":0.95}'>
          Hover and press me
        </button>
      </div>

      <div class="card" data-motion-initial='{"opacity":0,"x":-60}' data-motion-animate='{"opacity":1,"x":0}' data-motion-transition='{"type":"spring","bounce":0.4}'>
        <h2>Explicit states</h2>
        <p>Initial, animate and a spring transition, all as attributes.</p>
      </div>

      <div class="card">
        <h2>Scroll linked</h2>
        <div
          data-motion-scroll-animate='{"scaleX":[0,1]}'
          data-motion-scroll-offset='["start end","end start"]'
          style="height:8px;background:#38bdf8;transform-origin:left"
        ></div>
      </div>

      <div style="height:60vh"></div>

      <div class="card" data-motion="fade-up" data-motion-once="false">
        <h2>Repeats</h2>
        <p>This block animates every time it enters the viewport.</p>
      </div>
    </>
  );
}

export default MotionDemo;
