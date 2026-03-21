const { test } = require('playwright/test');

test('smoke loads frontend', async ({ page }) => {
  page.on('console', (msg) => console.log(`console:${msg.type()}: ${msg.text()}`));
  page.on('pageerror', (err) => console.log(`pageerror: ${err.stack || err.message}`));
  await page.goto('http://localhost:5174', { waitUntil: 'networkidle' });
  await page.locator('#auth-shell').waitFor();
});
