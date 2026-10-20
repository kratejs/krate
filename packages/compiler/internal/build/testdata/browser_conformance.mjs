import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import { spawn } from "node:child_process";

const root = process.argv[2];
const chromePath = process.argv[3];
const port = 4189;
const debugPort = 9349;
const pageUrl = `http://127.0.0.1:${port}/react-demo/`;

if (!root || !chromePath) {
  console.error("usage: browser_conformance.mjs <dist-root> <chrome>");
  process.exit(2);
}

const mime = {
  ".html": "text/html",
  ".js": "text/javascript",
  ".css": "text/css",
  ".json": "application/json",
  ".svg": "image/svg+xml",
  ".woff2": "font/woff2",
};
const server = http.createServer((req, res) => {
  let p = decodeURIComponent(req.url.split("?")[0]);
  if (p.endsWith("/")) p += "index.html";
  fs.readFile(path.join(root, p), (err, body) => {
    if (err) {
      res.statusCode = 404;
      res.end("not found");
      return;
    }
    res.setHeader("content-type", mime[path.extname(p)] || "application/octet-stream");
    res.end(body);
  });
});
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

const chrome = spawn(
  chromePath,
  [
    "--headless=new",
    "--disable-gpu",
    "--no-sandbox",
    "--disable-dev-shm-usage",
    "--no-first-run",
    "--no-default-browser-check",
    `--remote-debugging-port=${debugPort}`,
    `--user-data-dir=${path.join(process.env.TEMP || "/tmp", "krate-browser-" + Date.now())}`,
    "about:blank",
  ],
  { stdio: "ignore" },
);

let socket;
try {
  await new Promise((resolve) => server.listen(port, "127.0.0.1", resolve));
  let targets;
  for (let i = 0; i < 50; i++) {
    try {
      targets = await (await fetch(`http://127.0.0.1:${debugPort}/json`)).json();
      if (targets.some((t) => t.type === "page")) break;
    } catch {}
    await sleep(200);
  }
  const page = targets && targets.find((t) => t.type === "page");
  if (!page) throw new Error("Chrome did not expose a page target");

  socket = new WebSocket(page.webSocketDebuggerUrl);
  await new Promise((resolve, reject) => {
    socket.onopen = resolve;
    socket.onerror = reject;
  });
  let id = 0;
  const pending = new Map();
  const errors = [];
  socket.onmessage = (msg) => {
    const data = JSON.parse(msg.data);
    if (data.id && pending.has(data.id)) {
      pending.get(data.id)(data);
      pending.delete(data.id);
      return;
    }
    if (data.method === "Runtime.exceptionThrown") {
      errors.push(data.params.exceptionDetails.exception?.description || data.params.exceptionDetails.text);
    }
    if (data.method === "Runtime.consoleAPICalled" && data.params.type === "error") {
      errors.push(data.params.args.map((a) => a.value || a.description).join(" "));
    }
  };
  const send = (method, params = {}) =>
    new Promise((resolve) => {
      const callID = ++id;
      pending.set(callID, resolve);
      socket.send(JSON.stringify({ id: callID, method, params }));
    });
  const evaluate = async (expression) => {
    const result = await send("Runtime.evaluate", { expression, returnByValue: true, awaitPromise: true });
    return result.result?.result?.value;
  };

  await send("Page.enable");
  await send("Runtime.enable");
  await send("Page.navigate", { url: pageUrl });
  await sleep(2300);

  const before = await evaluate(`({
    state: document.querySelector('[role=switch]')?.getAttribute('data-state'),
    label: (document.body.innerText.match(/Live switch \\w+/) || [])[0],
    timer: (document.body.innerText.match(/timer: \\d+s/) || [])[0],
    classTick: Number((document.body.innerText.match(/Class component ticked (\\d+) times/) || [])[1])
  })`);
  await evaluate(`document.querySelector('[role=switch]').click()`);
  await sleep(400);
  await evaluate(`(() => { const b = Array.from(document.querySelectorAll('button')).find((x) => x.textContent.trim() === 'tick'); if (b) b.click(); })()`);
  await sleep(150);
  const after = await evaluate(`({
    state: document.querySelector('[role=switch]')?.getAttribute('data-state'),
    label: (document.body.innerText.match(/Live switch \\w+/) || [])[0],
    classTick: Number((document.body.innerText.match(/Class component ticked (\\d+) times/) || [])[1])
  })`);

  const box = await evaluate(`(() => {
    const b = Array.from(document.querySelectorAll('button')).find((x) => x.textContent === 'Hover me');
    if (!b) return null;
    b.scrollIntoView({ block: 'center' });
    const r = b.getBoundingClientRect();
    return { x: r.x + r.width / 2, y: r.y + r.height / 2 };
  })()`);
  if (!box) throw new Error("tooltip trigger not found");
  await send("Input.dispatchMouseEvent", { type: "mouseMoved", x: box.x, y: box.y });
  await sleep(450);
  const tooltip = await evaluate(`(() => {
    const t = document.querySelector('[role=tooltip]');
    return t ? t.textContent : null;
  })()`);

  const failures = [];
  if (before?.state !== "checked") failures.push(`switch initial state: ${before?.state}`);
  if (before?.label !== "Live switch on") failures.push(`switch initial label: ${before?.label}`);
  if (!before?.timer) failures.push("timer did not start");
  if (after?.state !== "unchecked") failures.push(`switch state after click: ${after?.state}`);
  if (after?.label !== "Live switch off") failures.push(`switch label after click: ${after?.label}`);
  if (!(after?.classTick > before?.classTick)) failures.push(`class component tick: ${before?.classTick} -> ${after?.classTick}`);
  if (tooltip !== "Tooltip content") failures.push(`tooltip content: ${tooltip}`);
  if (errors.length) failures.push("console errors: " + errors.join(" | "));

  console.log(JSON.stringify({ before, after, tooltip, errors, failures }, null, 2));
  if (failures.length) process.exitCode = 1;
} finally {
  if (socket) socket.close();
  chrome.kill();
  server.close();
}
