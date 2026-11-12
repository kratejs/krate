// The benchmark page: a list, a button that calls host code and a one-time
// first-paint report. Same behavior as the Krate and Tauri pages.
function measureFirstPaint(send) {
  let sent = false;
  const finish = (fcp) => {
    if (sent) return;
    sent = true;
    send(
      JSON.stringify({
        fcp,
        timeOrigin: performance.timeOrigin,
        paintAtMs: performance.timeOrigin + fcp,
        reportedAtMs: Date.now(),
        userAgent: navigator.userAgent,
      }),
    );
  };
  try {
    const po = new PerformanceObserver((list) => {
      for (const e of list.getEntries()) {
        if (e.name === 'first-contentful-paint') {
          po.disconnect();
          finish(e.startTime);
        }
      }
    });
    po.observe({ type: 'paint', buffered: true });
  } catch {
    // no paint timing: the timer below reports the second frame
  }
  setTimeout(() => requestAnimationFrame(() => requestAnimationFrame(() => finish(performance.now()))), 10000);
}

const list = document.getElementById('list');
for (let i = 1; i <= 100; i++) {
  const li = document.createElement('li');
  li.textContent = 'Item ' + i;
  list.appendChild(li);
}

let clicks = 0;
document.getElementById('ping').addEventListener('click', async () => {
  const n = clicks;
  document.getElementById('reply').textContent = await window.host.ping(n);
  clicks = n + 1;
});

measureFirstPaint((record) => {
  window.host.report(record);
});
