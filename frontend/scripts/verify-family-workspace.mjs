// Real browser with isolated API fixtures. Production Core/JWT verification is separate.
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { pathToFileURL } from 'node:url'
import { createBusinessFixture } from '../src/views/business/browser.fixture.mjs'

const native = process.argv.includes('--native') ? JSON.parse(readFileSync(0, 'utf8')) : undefined
const executablePath = native?.chromium || process.env.BROWSERSKILL_TEST_CHROMIUM
const playwrightPath = native?.playwright || process.env.BROWSERSKILL_TEST_PLAYWRIGHT
assert.ok(executablePath && playwrightPath, 'Set BROWSERSKILL_TEST_CHROMIUM and BROWSERSKILL_TEST_PLAYWRIGHT')
const { chromium } = await import(pathToFileURL(playwrightPath).href)
const fixture = native ? undefined : await createBusinessFixture()
let browser
try {
  browser = await chromium.launch({ executablePath, headless: true })
  if (native) await verifyNative(browser, native)
  else await verifyFixture(browser, fixture)
} finally {
  await browser?.close()
  await fixture?.server.close()
}

async function verifyFixture(browser, fixture) {
  const errors = []
  const viewer = await browser.newPage()
  viewer.on('pageerror', error => errors.push(error.message))
  await viewer.goto(fixture.url + '/platform/families?kb=kb-a&viewer=1')
  await viewer.getByRole('heading', { name: '研发协作制度族', exact: true }).waitFor()
  await viewer.getByRole('button', { name: '关联建议', exact: true }).click()
  await viewer.getByText('本地模型比较标题向量', { exact: true }).waitFor()
  assert.equal(await viewer.getByRole('button', { name: '确认建议', exact: true }).count(), 0)
  assert.equal(await viewer.getByRole('button', { name: '扫描本库', exact: true }).count(), 0)
  assert.equal(await viewer.getByRole('checkbox').count(), 0)
  assert.equal(fixture.calls.length, 0)
  await viewer.close()

  const admin = await browser.newPage()
  admin.on('pageerror', error => errors.push(error.message))
  await admin.goto(fixture.url + '/platform/families?kb=kb-a')
  await admin.getByRole('heading', { name: '研发协作制度族', exact: true }).waitFor()
  await admin.getByRole('button', { name: '关联建议', exact: true }).click()
  await admin.getByText('本地模型比较标题向量', { exact: true }).waitFor()
  for (const checkbox of await admin.getByRole('checkbox').all()) await checkbox.check()
  admin.once('dialog', dialog => dialog.accept())
  await admin.getByRole('button', { name: '批量确认（2）', exact: true }).click()
  await admin.getByText('已确认 2 条建议。', { exact: true }).waitFor()
  const writes = fixture.calls.filter(call => call.action === 'family_suggestion_confirm')
  assert.deepEqual(writes.map(call => call.body.business.id), ['suggestion-member', 'suggestion-relation'])
  assert.equal(new Set(writes.map(call => call.body.operation_id)).size, 2)
  await admin.getByLabel('知识库', { exact: true }).selectOption('kb-b')
  await admin.getByRole('heading', { name: '产品说明文档族', exact: true }).waitFor()
  assert.equal(await admin.getByText('研发协作制度族', { exact: true }).count(), 0)
  assert.equal(await admin.getByText('本地模型比较标题向量', { exact: true }).count(), 0)
  assert.deepEqual(errors, [])
  process.stdout.write('Family workspace browser fixture passed: semantic explanation, viewer restrictions, batch confirmation, KB switch\n')
}

async function verifyNative(browser, input) {
  const origin = new URL(input.origin)
  assert.equal(origin.protocol, 'http:')
  assert.equal(origin.hostname, '127.0.0.1')
  const query = new URLSearchParams({ kb: input.kb, family: input.family })
  const path = origin.origin + '/platform/families?' + query
  for (const [role, token] of [['viewer', input.viewer], ['admin', input.admin]]) {
    const context = await browser.newContext({ locale: 'zh-CN' })
    try {
      await context.addInitScript(({ jwt, tenant, coreOrigin }) => {
        if (location.origin !== coreOrigin) return
        localStorage.setItem('weknora_token', jwt)
        localStorage.setItem('weknora_selected_tenant_id', tenant)
        localStorage.setItem('locale', 'zh-CN')
        localStorage.setItem('weknora:new-user-guide-done:v1', '1')
        localStorage.setItem('weknora:assistant-entry-guide-done:v0.9.0', '1')
      }, { jwt: token, tenant: String(input.tenant), coreOrigin: origin.origin })
      const page = await context.newPage()
      await page.goto(path, { waitUntil: 'domcontentloaded' })
      await page.getByRole('heading', { name: 'Employee handbook', exact: true }).waitFor()
      await page.getByRole('button', { name: '关联建议', exact: true }).click()
      await page.getByText('本地模型比较标题向量', { exact: true }).first().waitFor()
      const scan = page.getByRole('button', { name: '扫描本库', exact: true })
      assert.equal(await scan.count(), role === 'admin' ? 1 : 0)
      if (role === 'admin') {
        await page.getByRole('tab', { name: '全部文档', exact: true }).click()
        await page.getByRole('heading', { name: '文档工作台', exact: true }).waitFor()
        assert.equal(await page.locator('a[href="/platform/families"],a[href="/platform/lifecycle"],a[href="/platform/lineage"],a[href="/platform/associations"],a[href="/platform/feedback-governance"],a[href="/platform/revisions"]').count(), 0)
        await page.getByRole('searchbox', { name: '文档标题', exact: true }).fill('Employee handbook')
        await page.getByRole('button', { name: 'Employee handbook 2025', exact: true }).click()
        const drawer = page.getByRole('dialog', { name: 'Employee handbook 2025' })
        await drawer.getByRole('button', { name: '保存日期', exact: true }).waitFor()
        await drawer.getByRole('button', { name: '关系图', exact: true }).click()
        await drawer.getByRole('heading', { name: '文档关系图', exact: true }).waitFor()
        await drawer.getByRole('button', { name: '治理记录', exact: true }).click()
        await drawer.getByText('加入版本族', { exact: true }).waitFor()
        await drawer.getByRole('button', { name: '关闭文档详情', exact: true }).click()
        if (input.hasFeedback) {
          await page.getByRole('tab', { name: '反馈审核', exact: true }).click()
          await page.getByRole('heading', { name: '反馈审核', exact: true }).waitFor()
          await page.getByLabel('审核状态').selectOption('active')
          await page.getByRole('button', { name: /fixture-turn/ }).click()
          await page.getByText('A shared answer excerpt.', { exact: true }).waitFor()
          await page.getByText('已核验的引用文档', { exact: true }).waitFor()
        }
        await page.goto(path, { waitUntil: 'domcontentloaded' })
        await page.getByLabel('知识库', { exact: true }).selectOption(input.other)
        await page.getByText('无法读取文档族列表。', { exact: true }).waitFor()
        assert.equal(await page.getByRole('heading', { name: 'Employee handbook', exact: true }).count(), 0)
      }
    } finally {
      await context.close()
    }
  }
  process.stdout.write('Native family browser passed: isolated JWT sessions, scoped titles, semantic evidence and viewer restrictions\n')
}
