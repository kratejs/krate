// Usage: node tailwind_layers.cjs <outDir> [chromium|firefox|webkit]. Serves outDir over HTTP and checks computed styles.
const path = require("path");
const http = require("http");
const fs = require("fs");
const playwright = require("playwright");

(async () => {
  const outDir = process.argv[2];
  const types = { ".html": "text/html", ".css": "text/css", ".js": "text/javascript" };
  const server = http.createServer((req, res) => {
    let p = decodeURIComponent(req.url.split("?")[0]);
    if (p.endsWith("/")) p += "index.html";
    const f = path.join(outDir, path.normalize(p));
    if (!f.startsWith(outDir) || !fs.existsSync(f)) { res.writeHead(404); res.end(); return; }
    res.writeHead(200, { "content-type": types[path.extname(f)] || "application/octet-stream" });
    res.end(fs.readFileSync(f));
  });
  await new Promise((r) => server.listen(0, "127.0.0.1", r));
  const browser = await playwright[process.argv[3] || "chromium"].launch();
  const page = await browser.newPage();
  await page.goto("http://127.0.0.1:" + server.address().port + "/");
  const cs = await page.evaluate(() => {
    const g = (id, p) => getComputedStyle(document.getElementById(id))[p];
    return {
      h1: g("h", "fontSize"),
      a: g("a", "color"),
      ul: g("ul", "paddingLeft"),
      btnPad: g("b", "paddingTop"),
      btnBg: g("b", "backgroundColor"),
      h2: g("h2", "fontSize"),
      util: g("u", "paddingTop"),
      utilColor: g("u", "color"),
      // The text-red-500 value as this browser serializes it (oklch forms
      // differ between engines), from an element styled inline.
      redRef: (() => {
        const el = document.createElement("span");
        el.style.color = "oklch(63.7% 0.237 25.331)";
        document.body.appendChild(el);
        const v = getComputedStyle(el).color;
        el.remove();
        return v;
      })(),
      brand: g("u", "backgroundColor"),
      apply: g("btnx", "paddingLeft"),
    };
  });
  await browser.close();
  server.close();
  const want = {
    h1: "40px", a: "rgb(255, 0, 0)", ul: "30px", btnPad: "10px", btnBg: "rgb(0, 128, 0)",
    h2: "33px", util: "24px", utilColor: cs.redRef, brand: "rgb(255, 0, 0)", apply: "16px",
  };
  let bad = 0;
  for (const k of Object.keys(want)) {
    if (cs[k] !== want[k]) { console.error(`${k}: got ${cs[k]}, want ${want[k]}`); bad++; }
  }
  console.log(JSON.stringify(cs));
  process.exit(bad ? 1 : 0);
})().catch((e) => { console.error(e); process.exit(2); });
