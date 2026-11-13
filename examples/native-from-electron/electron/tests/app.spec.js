// Playwright for Electron. The Krate equivalent is tests/port.native.test.ts.
const { test, expect, _electron: electron } = require('@playwright/test');

test('typing saves a draft and the settings window changes the theme', async () => {
  const app = await electron.launch({ args: ['.'] });
  const page = await app.firstWindow();
  await page.fill('#note', 'hello');
  await expect.poll(() => page.evaluate(() => window.scratch.loadDraft())).toBe('hello');

  await page.evaluate(() => window.scratch.updateSettings({ theme: 'dark' }));
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await app.close();
});
