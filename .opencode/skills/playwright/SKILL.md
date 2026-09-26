---
name: Playwright Browser Automation
description: Automate web browsers using Playwright (Node.js). Use for screenshots, page interaction, form filling, testing web pages, or extracting content from websites.
---

## Playwright Browser Automation

Playwright 1.63.0 is installed globally with Node.js bindings. Browsers available: Chromium, Firefox, WebKit.

### Quick Screenshot

```bash
node -e "
const { chromium } = require('playwright');
(async () => {
  const browser = await chromium.launch();
  const page = await browser.newPage();
  await page.goto('https://example.com');
  await page.screenshot({ path: '/tmp/screenshot.png' });
  await browser.close();
})();
"
```

### View Page Content

```bash
node -e "
const { chromium } = require('playwright');
(async () => {
  const browser = await chromium.launch();
  const page = await browser.newPage();
  await page.goto('https://example.com');
  const content = await page.content();
  console.log(content);
  await browser.close();
})();
"
```

### Fill a Form and Submit

```bash
node -e "
const { chromium } = require('playwright');
(async () => {
  const browser = await chromium.launch();
  const page = await browser.newPage();
  await page.goto('https://example.com/login');
  await page.fill('#username', 'user');
  await page.fill('#password', 'pass');
  await page.click('button[type=submit]');
  await page.waitForSelector('.welcome');
  console.log('Logged in:', await page.title());
  await browser.close();
})();
"
```

### Full Page Screenshot

```bash
node -e "
const { chromium } = require('playwright');
(async () => {
  const browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1280, height: 720 } });
  await page.goto('https://example.com');
  await page.screenshot({ path: '/tmp/fullpage.png', fullPage: true });
  await browser.close();
})();
"
```

### Click and Wait for Navigation

```bash
node -e "
const { chromium } = require('playwright');
(async () => {
  const browser = await chromium.launch();
  const page = await browser.newPage();
  await page.goto('https://example.com');
  await Promise.all([
    page.waitForNavigation(),
    page.click('a[href=\"/about\"]')
  ]);
  console.log('Navigated to:', page.url());
  await browser.close();
})();
"
```

### Use a Different Browser

Replace `chromium` with `firefox` or `webkit`:

```bash
node -e "
const { firefox } = require('playwright');
(async () => {
  const browser = await firefox.launch();
  // ... same API
})();
"
```

### Headed Mode (with UI)

```bash
node -e "
const { chromium } = require('playwright');
(async () => {
  const browser = await chromium.launch({ headless: false });
  // ...
})();
"
```

### Common Selectors

- `page.fill('input[name=email]', 'user@example.com')` - Fill by CSS
- `page.click('text=Submit')` - Click by text
- `page.click('button:has-text(\"Go\")')` - Click button with text
- `page.waitForSelector('.loaded')` - Wait for element

### Tips

- Screenshots go to `/tmp/screenshot.png` by default
- Use `fullPage: true` for full page captures
- Use `page.evaluate()` to run JavaScript in the page context
- Use `page.waitForTimeout(1000)` for timing waits (prefer selectors when possible)
- For complex scripts, write a `.js` file and run `node script.js`