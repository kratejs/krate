// A server page with an asChild Link beside a client island, reached by a
// soft navigation (see TestBrowserAsChildSoftNav), must match a full load.
import { chromium, firefox, webkit } from "playwright";
import { waitHydrated } from "./island_helpers.mjs";

const browserType = { chromium, firefox, webkit }[process.argv[2] || "chromium"];
const base = process.argv[3];
const browser = await browserType.launch({ headless: true });
const failures = [];
const shape = (page) =>
  page.evaluate(() => ({
    links: [...document.querySelectorAll(".review, td a, td span.v, td em, td [data-slot=badge]")].map((a) => a.tagName + ":" + a.getAttribute("href")),
    ages: [...document.querySelectorAll(".age")].map((a) => a.textContent),
    markers: document.evaluate(".//comment()[starts-with(., 'krate-slot:') or starts-with(., '/krate-slot:')]", document.body, null, 7, null).snapshotLength,
  }));
try {
  const context = await browser.newContext();
  const errors = [];
  const open = async () => {
    const page = await context.newPage();
    page.on("pageerror", (e) => errors.push("pageerror: " + e.message));
    page.on("console", (m) => {
      if (m.type() === "error") errors.push("console: " + m.text());
    });
    return page;
  };
  // What a full load of each tab shows.
  const want = {};
  for (const tab of ["a", "b", "c", "d", "e", "f", "g"]) {
    const page = await open();
    await page.goto(base + "/table" + (tab === "a" ? "" : "?tab=" + tab));
    if (tab === "c") await page.waitForSelector("#empty");
    else await waitHydrated(page, tab === "a" ? null : ".age[data-ready=yes]");
    want[tab] = await shape(page);
    await page.close();
  }
  if (want.a.links.length !== 9 || want.b.links.length !== 11) failures.push("full loads lack the Review links: " + JSON.stringify(want));

  // From another page onto the table, then between its tabs, by clicking.
  const page = await open();
  await page.goto(base + "/");
  await page.click("#to-table");
  await page.waitForSelector("td.when", { timeout: 5000 });
  for (const tab of ["b", "f", "g", "b", "f", "e", "a", "d", "c", "b", "d"]) {
    await page.hover("#tab-" + tab);
    await page.waitForTimeout(400);
    await page.click("#tab-" + tab);
    await page.waitForFunction((t) => location.search === (t === "a" ? "" : "?tab=" + t), tab, { timeout: 5000 });
    await page.waitForTimeout(500);
    const got = await shape(page);
    if (JSON.stringify(got) !== JSON.stringify(want[tab])) failures.push("soft navigation to tab " + tab + " differs from a full load: " + JSON.stringify({ want: want[tab], got }));
  }
  for (const dir of ["goBack", "goBack", "goForward", "goForward", "goBack"]) {
    await page[dir]();
    await page.waitForTimeout(700);
    const tab = new URL(page.url()).searchParams.get("tab") || "a";
    const got = await shape(page);
    if (JSON.stringify(got) !== JSON.stringify(want[tab])) failures.push(dir + " to tab " + tab + " differs from a full load: " + JSON.stringify({ want: want[tab], got }));
  }
  await page.click("#tab-b");
  await page.waitForTimeout(700);
  try {
    await waitHydrated(page, ".age[data-ready=yes]");
  } catch (e) {
    failures.push("the island did not hydrate after the soft navigations");
  }
  await page.click("a.review");
  try {
    await page.waitForSelector("#item", { timeout: 5000 });
  } catch (e) {
    failures.push("clicking Review did not navigate: " + page.url());
  }
  // Starting on a tab (hydrated from the document), away and back.
  const round = await open();
  await round.goto(base + "/table?tab=b");
  await waitHydrated(round, ".age[data-ready=yes]");
  for (const tab of ["a", "b", "c", "b", "e", "b"]) {
    await round.click("#tab-" + tab);
    await round.waitForFunction((t) => location.search === (t === "a" ? "" : "?tab=" + t), tab, { timeout: 5000 });
    await round.waitForTimeout(500);
    const got = await shape(round);
    if (JSON.stringify(got) !== JSON.stringify(want[tab])) failures.push("from a loaded tab to " + tab + " differs from a full load: " + JSON.stringify({ want: want[tab], got }));
  }
  await round.close();
  // From another page straight onto a tab.
  const direct = await open();
  await direct.goto(base + "/");
  await direct.click("#to-b");
  await direct.waitForFunction(() => location.search === "?tab=b", null, { timeout: 5000 });
  await direct.waitForTimeout(1000);
  const got = await shape(direct);
  if (JSON.stringify(got) !== JSON.stringify(want.b)) failures.push("navigation from another page differs from a full load: " + JSON.stringify({ want: want.b, got }));
  await direct.close();
  // Navigating while the islands are still loading.
  for (const [from, tab] of [["a", "b"], ["b", "a"], ["d", "b"]]) {
    const fast = await open();
    await fast.goto(base + "/table" + (from === "a" ? "" : "?tab=" + from), { waitUntil: "commit" });
    await fast.waitForSelector("#tab-" + tab);
    await fast.click("#tab-" + tab);
    await fast.waitForFunction((t) => location.search === (t === "a" ? "" : "?tab=" + t), tab, { timeout: 5000 });
    await fast.waitForTimeout(1500);
    const got = await shape(fast);
    if (JSON.stringify(got) !== JSON.stringify(want[tab])) failures.push("early navigation " + from + " to " + tab + " differs from a full load: " + JSON.stringify({ want: want[tab], got }));
    await fast.close();
  }
  if (errors.length) failures.push(errors.join("\n"));
} finally {
  await browser.close();
}
if (failures.length) {
  console.error(failures.join("\n"));
  process.exit(1);
}
console.log("ok");
