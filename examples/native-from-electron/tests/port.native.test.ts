// Run with: krate native test   (on a headless Linux machine: xvfb-run -a krate native test)
// The Krate version of electron/tests/app.spec.js, and of the whole Electron app.
import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { launch, poll, type App, type Page } from "@krate/app/driver";

let app: App;
let page: Page;

before(async () => {
  app = await launch();
  page = app.page();
  await page.waitFor("#note");
});

after(async () => {
  await app?.close();
});

test("typing saves a draft through a host function", async () => {
  await page.fill("#note", "hello from the draft");
  await poll(async () => (await page.call<string>("src/lib/store.host.loadDraft")) === "hello from the draft");
  const dir = await page.call<string>("krate/os.appDataDir");
  assert.equal(readFileSync(join(dir, "draft.txt"), "utf8"), "hello from the draft");
});

test("files are read and written through the fs API, inside its scope", async () => {
  const dir = await page.call<string>("krate/os.appDataDir");
  const path = join(dir, "out.txt");
  await page.evaluate((p: string) => (globalThis as any).__scratch.saveFile(p, "saved by the page"), path);
  await page.waitFor(() => document.getElementById("status")?.textContent?.startsWith("saved"));
  assert.equal(readFileSync(path, "utf8"), "saved by the page");

  writeFileSync(path, "changed on disk");
  await page.evaluate((p: string) => (globalThis as any).__scratch.loadFile(p), path);
  await page.waitFor(() => document.getElementById("status")?.textContent?.startsWith("opened"));
  assert.equal(await page.value("#note"), "changed on disk");
  assert.equal(await page.text("#file"), path);

  // A path outside the app directories that no dialog granted is refused.
  const outside = mkdtempSync(join(tmpdir(), "scratchpad-outside-"));
  try {
    writeFileSync(join(outside, "secret.txt"), "not for the page");
    await page.evaluate((p: string) => (globalThis as any).__scratch.loadFile(p), join(outside, "secret.txt"));
    await page.waitFor(() => document.getElementById("status")?.textContent === "error permission_denied");
    assert.equal(await page.value("#note"), "changed on disk");
  } finally {
    rmSync(outside, { recursive: true, force: true });
  }
});

test("the New menu item clears the note", async () => {
  // The page subscribes after it hydrates; emit until it has heard one.
  await poll(async () => {
    await app.emit.menu("file.new");
    return (await page.text("#status")) === "new note";
  });
  assert.equal(await page.value("#note"), "");
});

test("the settings window changes the theme of the main window and is stored", async () => {
  assert.equal(await page.evaluate(() => document.querySelector("main")?.getAttribute("data-theme")), "light");
  await poll(async () => {
    await app.emit.menu("file.settings");
    return (await app.windows()).length >= 2;
  });
  const settingsWindow = (await app.windows()).find((w) => w.id !== "main");
  assert.ok(settingsWindow, "a second window");
  const settings = app.page(settingsWindow.id);
  await settings.waitFor("#theme");
  await settings.fill("#theme", "dark");
  await page.waitFor(() => document.querySelector("main")?.getAttribute("data-theme") === "dark");
  assert.deepEqual(await page.call("krate/kv.get", "settings"), { theme: "dark", fontSize: 16 });

  // A handler that throws rejects the call, as ipcRenderer.invoke does.
  await settings.fill("#font-size", "99");
  await settings.waitFor(() => document.getElementById("error")?.textContent?.startsWith("fontSize must be 10 to 32"));
});

test("tray events reach the page", async () => {
  await poll(async () => {
    await app.emit.tray("click");
    return (await page.text("#tray-status")) === "tray click";
  });
  await poll(async () => {
    await app.emit.tray("menu", "tray.show");
    return (await page.text("#tray-status")) === "tray menu tray.show";
  });
});

test("a notification is sent, or reported as unsupported", async () => {
  await page.fill("#note", "12345");
  await page.click("#notify");
  await page.waitFor(() => document.getElementById("notify-status")?.textContent !== "");
  // Linux without a notification service answers "unsupported"; everywhere else it is sent.
  assert.match(await page.text("#notify-status"), /^notification (sent|unsupported)$/);
});

test("no window threw an error", async () => {
  for (const v of await app.views()) assert.deepEqual(await app.page(v.id).errors(), [], v.id);
});
