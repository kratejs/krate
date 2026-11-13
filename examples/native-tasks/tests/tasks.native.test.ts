// Run with: krate native test   (on a headless Linux machine: xvfb-run -a krate native test)
import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import { launch, type App, type Page } from "@krate/app/driver";

let app: App;
let page: Page;

before(async () => {
  app = await launch();
  page = app.page();
  await page.waitFor("#add");
});

after(async () => {
  await app?.close();
});

test("adds a task through the form", async () => {
  assert.equal(await page.count("#list li"), 1);
  await page.fill("#title", "Buy milk");
  await page.click("#add");
  await page.waitFor(() => document.querySelectorAll("#list li").length === 2);
  assert.match(await page.text("#list"), /Buy milk/);
  assert.equal(await page.text("#open"), "Open tasks: 2");
});

test("toggling a task updates the open count", async () => {
  await page.click("#list li:first-child .toggle");
  await page.waitFor(() => document.getElementById("open")?.textContent === "Open tasks: 1");
  assert.equal(await page.text("#list li:first-child .toggle"), "done");
});

test("a host error reaches the page as a KrateError", async () => {
  await page.click("#missing");
  await page.waitFor(() => document.getElementById("error")?.textContent?.startsWith("not_found"));
  assert.equal(await page.text("#error"), "not_found: no task 9999");
});

test("host functions can be called as the page does", async () => {
  const tasks = await page.call<{ id: number; title: string }[]>("src/lib/tasks.List");
  assert.deepEqual(tasks.map((t) => t.title), ["Read the Krate Native docs", "Buy milk"]);
});

test("the page threw no errors", async () => {
  assert.deepEqual(await page.errors(), []);
});
