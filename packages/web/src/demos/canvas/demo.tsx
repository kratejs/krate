import { createSignal, createEffect, onMount, onCleanup } from "@krate/runtime";
import "./demo.css";

type Dot = { x: number; y: number; vx: number; vy: number };

// A canvas animation driven by signals. The effect re-runs when `running`
// flips: it starts a requestAnimationFrame loop and onCleanup cancels it.
// Users who prefer reduced motion start paused with one static frame.
export default function Demo() {
  const calm = typeof window !== "undefined" && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  const [running, setRunning] = createSignal(!calm);
  const [count, setCount] = createSignal(80);
  const [speed, setSpeed] = createSignal(1);
  const [fps, setFps] = createSignal(0);
  const [ready, setReady] = createSignal(false);
  let canvas: HTMLCanvasElement | undefined;
  const dots: Dot[] = [];
  let width = 0;
  let height = 0;
  let pointer: { x: number; y: number } | null = null;

  const fit = () => {
    const c = canvas!;
    const ratio = window.devicePixelRatio || 1;
    width = c.clientWidth;
    height = c.clientHeight;
    c.width = width * ratio;
    c.height = height * ratio;
    c.getContext("2d")!.setTransform(ratio, 0, 0, ratio, 0, 0);
  };

  const draw = () => {
    const ctx = canvas!.getContext("2d")!;
    const color = getComputedStyle(canvas!).getPropertyValue("--accent").trim() || "#c2410c";
    ctx.clearRect(0, 0, width, height);
    ctx.fillStyle = color;
    ctx.strokeStyle = color;
    for (let i = 0; i < dots.length; i++) {
      const a = dots[i];
      ctx.globalAlpha = 0.9;
      ctx.beginPath();
      ctx.arc(a.x, a.y, 2, 0, 6.2832);
      ctx.fill();
      for (let j = i + 1; j < dots.length; j++) {
        const b = dots[j];
        const d = Math.hypot(a.x - b.x, a.y - b.y);
        if (d < 70) {
          ctx.globalAlpha = (1 - d / 70) * 0.5;
          ctx.beginPath();
          ctx.moveTo(a.x, a.y);
          ctx.lineTo(b.x, b.y);
          ctx.stroke();
        }
      }
    }
  };

  const step = () => {
    const k = speed();
    for (const d of dots) {
      if (pointer) {
        d.vx += (pointer.x - d.x) * 0.00004;
        d.vy += (pointer.y - d.y) * 0.00004;
      }
      d.x += d.vx * k;
      d.y += d.vy * k;
      if (d.x < 0 || d.x > width) d.vx *= -1;
      if (d.y < 0 || d.y > height) d.vy *= -1;
    }
  };

  onMount(() => {
    fit();
    const watch = new ResizeObserver(() => {
      fit();
      draw();
    });
    watch.observe(canvas!);
    onCleanup(() => watch.disconnect());
    setReady(true);
  });

  // Grow or shrink the swarm when the slider moves.
  createEffect(() => {
    const n = count();
    if (!ready()) return;
    while (dots.length < n) {
      dots.push({ x: Math.random() * width, y: Math.random() * height, vx: Math.random() - 0.5, vy: Math.random() - 0.5 });
    }
    dots.length = n;
    if (!running()) draw();
  });

  createEffect(() => {
    if (!ready()) return;
    if (!running()) {
      draw();
      return;
    }
    let frame = 0;
    let frames = 0;
    let last = performance.now();
    const tick = (now: number) => {
      step();
      draw();
      frames++;
      if (now - last >= 500) {
        setFps(Math.round((frames * 1000) / (now - last)));
        frames = 0;
        last = now;
      }
      frame = requestAnimationFrame(tick);
    };
    frame = requestAnimationFrame(tick);
    onCleanup(() => cancelAnimationFrame(frame));
  });

  return (
    <div class="cv">
      <canvas
        ref={canvas}
        class="cv-canvas"
        role="img"
        aria-label="Moving dots joined by lines when they are close"
        onPointerMove={(e) => {
          const r = canvas!.getBoundingClientRect();
          pointer = { x: e.clientX - r.left, y: e.clientY - r.top };
        }}
        onPointerLeave={() => (pointer = null)}
      ></canvas>
      <div class="cv-controls">
        <button type="button" class="dk-btn cv-run" aria-pressed={running()} onClick={() => setRunning(!running())}>
          Animate
        </button>
        <label class="cv-field">Dots <output>{count()}</output>
          <input type="range" min="10" max="240" value={count()} onInput={(e) => setCount(Number((e.target as HTMLInputElement).value))} />
        </label>
        <label class="cv-field">Speed <output>{speed().toFixed(1)}x</output>
          <input type="range" min="0.2" max="3" step="0.1" value={speed()} onInput={(e) => setSpeed(Number((e.target as HTMLInputElement).value))} />
        </label>
        <span class="dk-muted dk-mono cv-fps">{running() ? fps() + " fps" : "paused"}</span>
      </div>
    </div>
  );
}
