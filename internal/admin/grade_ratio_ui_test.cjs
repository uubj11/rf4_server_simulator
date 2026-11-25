const { test } = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const { chromium } = require('playwright');

test('grade ratio grips share the numeric draft and explicit save', async () => {
  const browser = await chromium.launch({ executablePath: process.env.RF4_TEST_BROWSER });
  try {
    const page = await browser.newPage({ hasTouch: true });
    await page.addInitScript(() => localStorage.setItem('rf4_admin_token', 'fixture'));
    const keys = ['below', 'qualified', 'trophy', 'super'].map(g => 'grade_pct_' + g);
    const settings = Object.fromEntries(keys.map((key, i) => [key, ['0', '60', '25', '15'][i]]));
    const writes = [];
    await page.route('**/*', async route => {
      const url = new URL(route.request().url());
      if (!url.pathname.startsWith('/api/')) {
        const file = url.pathname === '/' ? 'index.html' : url.pathname.slice(1);
        return route.fulfill({ path: path.join(__dirname, 'web', file) });
      }
      if (url.pathname === '/api/tunables') {
        if (route.request().method() === 'POST') {
          const { key, value } = route.request().postDataJSON();
          writes.push(key);
          settings[key] = value;
        }
        return route.fulfill({ json: settings });
      }
      return route.fulfill({ json: { username: 'admin' } });
    });
    await page.goto('http://localhost/#rules');
    const card = page.locator('.card').filter({ has: page.getByText('四档比例', { exact: true }) });
    const inputs = card.locator('input');
    const track = card.locator('.grade-ratio-track');
    const grips = card.getByRole('slider');
    const values = () => inputs.evaluateAll(nodes => nodes.map(n => Number(n.value)));
    const widths = () => track.locator(':scope > div > div').evaluateAll(nodes => nodes.map(n => parseFloat(n.style.width)));
    async function drag(i, fraction, checkDuringDrag) {
      await grips.nth(i).scrollIntoViewIfNeeded();
      const box = await grips.nth(i).boundingBox();
      const x = box.x + box.width / 2, y = box.y + box.height / 2;
      await page.mouse.move(x, y);
      await page.mouse.down();
      await page.mouse.move(x + fraction * (await track.boundingBox()).width, y, { steps: 5 });
      if (checkDuringDrag) await checkDuringDrag();
      await page.mouse.up();
    }
    // Fixed width makes a ten-point pointer movement exact in whole CSS pixels.
    await card.waitFor();
    const firstGripBox = await grips.first().boundingBox();
    assert.ok(firstGripBox.width >= 44, `grip width ${firstGripBox.width}`);
    assert.ok(firstGripBox.height >= 44, `grip height ${firstGripBox.height}`);
    await track.evaluate(el => { el.style.width = '500px'; });
    await drag(1, -0.1, async () => {
      assert.deepEqual(await values(), [0, 50, 35, 15]);
      assert.deepEqual(await widths(), [0, 50, 35, 15]);
    });
    assert.deepEqual(await values(), [0, 50, 35, 15]);
    assert.deepEqual(await widths(), [0, 50, 35, 15]);
    assert.deepEqual(writes, []);
    await page.getByRole('button', { name: '保存修改' }).click();
    await page.waitForSelector('.savebar.on', { state: 'hidden' });
    assert.deepEqual(writes, keys.slice(1, 3));
    await page.reload();
    await card.waitFor();
    assert.deepEqual(await values(), [0, 50, 35, 15]);

    const touch = await page.context().newCDPSession(page);
    await grips.first().scrollIntoViewIfNeeded();
    const grip = await grips.first().boundingBox();
    const x = grip.x + grip.width / 2, y = grip.y + grip.height / 2;
    await touch.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x, y }] });
    await touch.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x: x + 50, y }] });
    await touch.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
    const touched = await values();
    assert.ok(touched[0] > 0);
    assert.equal(touched[0] + touched[1], 50);
    await page.getByRole('button', { name: '放弃' }).click();

    await inputs.nth(3).fill('15.5');
    assert.ok(Math.abs((await widths())[3] - 15.5 / 100.5 * 100) < 0.001);
    await grips.nth(0).press('ArrowRight');
    assert.deepEqual(await values(), [1.005, 48.995, 35, 15.5]);
    await page.getByRole('button', { name: '放弃' }).click();
    assert.deepEqual(await widths(), [0, 50, 35, 15]);

    for (const input of await inputs.all()) await input.fill('0');
    assert.deepEqual(await widths(), [0, 0, 0, 0]);
    assert.equal(await card.getByRole('slider', { disabled: true }).count(), 3);
    await inputs.last().fill('80');
    for (const i of [2, 1, 0]) await drag(i, 0.1 * (i + 1));
    const recovered = await values();
    assert.ok(recovered.every(n => n > 0));
    assert.ok(Math.abs(recovered.reduce((a, b) => a + b) - 80) < 1e-9);

    await inputs.nth(0).fill('90');
    await inputs.nth(1).fill('40');
    await grips.first().press('End');
    assert.deepEqual((await values()).slice(0, 2), [100, 30]);
    assert.deepEqual(writes, keys.slice(1, 3));

    settings.grade_ratio_enabled = '0';
    settings.fish_hunt_enabled = '1';
    await page.reload();
    await page.getByText(/鱼猎四阶段按玩家打窝/).waitFor();
    assert.equal(await page.getByRole('slider').count(), 0);
    settings.two_stage_hunt_enabled = '1';
    await page.reload();
    for (const title of ['日常（没打窝）', '打窝后']) {
      const editor = page.locator('.card').filter({ has: page.getByText(title, { exact: true }) });
      await editor.waitFor();
      assert.equal(await editor.locator('input').count(), 4);
      await editor.locator('input').first().fill('0');
      await editor.getByText('0.0%', { exact: true }).first().waitFor();
    }
    assert.equal(await page.getByRole('slider').count(), 0);
    assert.deepEqual(writes, keys.slice(1, 3));
  } finally {
    await browser.close();
  }
});
