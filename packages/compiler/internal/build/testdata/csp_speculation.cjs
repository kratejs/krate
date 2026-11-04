// Usage: node csp_speculation.cjs <baseURL> [chromium|firefox|webkit]
// Loads a CSP-enabled page and fails on any CSP violation (the router injects
// an inline speculation-rules script at runtime unless a CSP meta is present).
const playwright = require("playwright");

(async () => {
  const browser = await playwright[process.argv[3] || "chromium"].launch();
  const page = await browser.newPage();
  const violations = [];
  await page.addInitScript(() => {
    window.__csp = [];
    document.addEventListener("securitypolicyviolation", (e) => {
      window.__csp.push(e.violatedDirective + " " + (e.blockedURI || "") + " " + (e.sample || ""));
    });
  });
  page.on("console", (m) => {
    if (/Content Security Policy|speculation/i.test(m.text())) violations.push(m.text());
  });
  await page.goto(process.argv[2] + "/");
  await page.waitForTimeout(1000);
  const res = await page.evaluate(() => ({
    csp: window.__csp,
    has: !!document.querySelector('script[type="speculationrules"]'),
    supported: typeof HTMLScriptElement.supports === "function" && HTMLScriptElement.supports("speculationrules"),
  }));
  await browser.close();
  console.log(JSON.stringify({ res, violations }));
  if (res.csp.length || violations.length) process.exit(1);
})().catch((e) => { console.error(e); process.exit(1); });
