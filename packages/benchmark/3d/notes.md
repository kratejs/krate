## Read this first

- **Software rendering.** There is no usable GPU here (`/dev/dri` has only a `card0` with no render node, and Chromium reports
  SwiftShader). Every pixel is rasterised on the CPU by SwiftShader, using a thread pool across all cores. Absolute numbers
  mean nothing for real hardware; only the relative comparison between the variants means something, and even that
  favours engines that draw fewer pixels and fewer draw calls more than a real GPU would.
- **Noisy machine.** The box is shared and was not idle (load average 7 at the start of the run, partly the benchmark's own
  SwiftShader threads). The medians of 3 are stable to a few percent for the heavy variants, much less so for `three-compiled`.
- **rAF intervals above 60 FPS are not trustworthy.** `three-compiled` shows a median interval of 0.4 ms but a p95 of about 80 ms:
  headless Chromium runs rAF in bursts when the scene is this cheap, so use the mean (1000 / FPS) rather than the median for it.
  Filament sits at about 17 ms, which looks like a 60 Hz cap rather than its true throughput, so its FPS is a lower bound.
  The main-thread ms per frame and draw/GL calls per frame are the more reliable comparisons.
- **GPU-process CPU per frame is inflated** by SwiftShader worker threads that spin while waiting, so compare it only roughly.
- `gl.finish()` is called at the start of every rAF in all variants (see README), and the first-frame mark comes from each engine's own
  frame hook, which is the closest equivalent point but not identical (three: after render, filament: at frame start).

## Observations

- **Draw calls dominate.** `three-compiled` draws the scene in 7 calls (1000 boxes as one `InstancedMesh`, the 100 ring spheres as one
  `InstancedMesh`, 4 special meshes, the plane). `three-runtime` makes 1105 draw calls. `@krate/3d` makes about 138 `drawElementsInstanced` calls for 1100+ separate renderables plus post passes; why the count is that low was not
  investigated (Filament may merge draws of renderables sharing a primitive and material, or cull), so treat it as an observation, not an explanation.
- **The compiled three path is by far the cheapest per frame** here (about 6 ms main thread, 7 draws) and the runtime path the most
  expensive (about 52 ms main thread, 1105 draws, 25 MB JS heap after GC versus about 4 MB).
- **Filament's per-frame main-thread cost is tiny (about 1 ms)**, since the compiled scene is direct typed-array writes into WASM
  structures; most of its cost is on the rasteriser side (more draws than the instanced three path, a post-process chain).
- **Animation is cheap in all three**: static and animated numbers differ by well under the noise, except for a bit more heap.
- **Payload is where Filament loses.** The page code is small (about 38 KB gzip with the runtime), but the engine is `filament.wasm`
  (3.4 MB raw, about 1.9 MB gzip) plus `filament.js` (about 51 KB gzip) plus materials (82 KB raw for the one used). The three
  variants ship about 238 KB gzip, of which about 230 KB is the engine and runtime baseline (the empty-Canvas page).
- **Memory.** Filament adds about 39 MB of WASM heap; total RSS across processes is close for filament and three-compiled
  (about 540-550 MB, of which about 95 MB is the browser process) and about 100 MB higher for three-runtime (its GPU process holds more).
- **Startup.** Filament's first frame is at about 250 ms (wasm download and instantiate plus material load, local server);
  three-compiled is the earliest (about 165-190 ms) and three-runtime the latest (about 380-410 ms).
- **@krate/3d fallback finding.** A `.map()` whose items have signal-driven props falls back to the runtime path in `@krate/3d` (KR8701
  "a non-constant position inside a list callback"), while `@krate/three` compiles it into an `InstancedMesh` with in-place matrix writes.
  The benchmark therefore writes the animated ring out as 100 generated meshes for Filament. No bug was fixed in the packages.

## Visual comparison

Screenshots are not committed. They were taken at `__freeze(0.5)` at 1280x720 and are in `/home/dev/wt/krate-3d-bench-shots/`
when produced by the author (`compare-static.png`, `compare-animated.png`, plus one file per variant and mode).

- Geometry, camera, framing and all objects match in all three: 1000 boxes, 4 colored meshes, 100 spheres, plane, no missing objects.
- **Brightness and color differ.** Filament is clearly brighter and more washed out: a light grey ground and near-white boxes, pastel
  red/blue/gold. three renders a darker blue-grey ground and boxes, with deeper saturated red/blue and a more orange ring. Using
  the documented intensity mapping, Filament comes out noticeably brighter than three at the same light numbers; a brightness-matched
  comparison would need an `exposure` offset (not applied, to keep the documented mapping).
- Filament shows slightly more specular on the teal sphere and a softer ambient fill, so the faces of the boxes are less
  contrasty. three has a stronger directional shading on box sides.
- Aliasing: antialiasing is off in all variants, so edges (box grid, sphere ring) are stair-stepped equally.
- `three-compiled` and `three-runtime` look the same (no pixel diff was run).
