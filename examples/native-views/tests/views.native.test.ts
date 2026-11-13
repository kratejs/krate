// Run with: krate native test   (on a headless Linux machine: xvfb-run -a krate native test)
import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import { launch, poll, type App } from "@krate/app/driver";

let app: App;

before(async () => {
  app = await launch();
});

after(async () => {
  await app?.close();
});

test("the main page opens a sidebar and a log view", async () => {
  // The first wait covers the cold start: the main page loads, hydrates and
  // only then opens the two views, which took over 5s on a CI runner.
  await app.waitForViewCount(3, { timeout: 30_000 });
  const windows = await app.windows();
  assert.deepEqual(
    windows.map((w) => [w.id, w.views.map((v) => v.id)]),
    [["main", ["main", "sidebar", "log"]]],
  );
  assert.deepEqual((await app.window("main").views()).filter((v) => v.primary).map((v) => v.id), ["main"]);
});

test("each view is its own page", async () => {
  const sidebar = app.window("main").view("sidebar");
  const log = app.page("log");
  await sidebar.waitFor(() => location.pathname.startsWith("/sidebar"));
  await log.waitFor(() => location.pathname.startsWith("/log"));
  assert.match(await sidebar.url(), /\/sidebar/);
  assert.match(await log.url(), /\/log/);
  const main = app.page("main");
  await main.waitFor(() => document.getElementById("loaded")?.textContent?.includes("sidebar"));
  assert.match(await main.text("#loaded"), /sidebar \d+x\d+/);
});

test("a menu event reaches the page that listens for it", async () => {
  // Send the sidebar view to the route that listens, then fire the event.
  const view = app.page("sidebar");
  await view.evaluate(() => {
    setTimeout(() => location.assign("/events"), 0);
  });
  await view.waitFor("#events");
  // The page subscribes after it hydrates; emit until it has heard one.
  await poll(async () => {
    await app.emit.menu("view.refresh");
    return (await view.text("#events")).includes("menu view.refresh");
  });
});

test("no view threw an error", async () => {
  for (const v of await app.views()) assert.deepEqual(await app.page(v.id).errors(), [], v.id);
});
