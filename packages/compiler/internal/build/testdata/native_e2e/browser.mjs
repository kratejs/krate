// Drives the native-tasks example served by krate-shell --browser.
// usage: node browser.mjs <url> <browser> <flow_main.js> <flow_task.js>
import { createRequire } from 'node:module';
import { readFileSync } from 'node:fs';

const require = createRequire(process.cwd() + '/');
const pw = require('playwright');
const [url, browserName, mainFlow, taskFlow] = process.argv.slice(2);

const browser = await pw[browserName].launch();
const page = await browser.newPage();
const errors = [];
page.on('console', (m) => {
  if (m.type() === 'error') errors.push('console: ' + m.text());
});
page.on('pageerror', (e) => errors.push('pageerror: ' + e.message));

const reports = [];
await page.goto(url);
reports.push(...(await page.evaluate(readFileSync(mainFlow, 'utf8'))));
const base = new URL(url).origin;
for (const path of ['/tasks/2', '/tasks/77']) {
  await page.goto(base + path);
  reports.push(...(await page.evaluate(readFileSync(taskFlow, 'utf8'))));
}
await browser.close();
console.log(JSON.stringify({ reports, errors }));
