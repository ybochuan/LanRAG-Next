// Exercise the production SPA with an isolated API fixture. The installed
// Windows candidate and its Wails/WebView2 bindings are verified separately.
import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { readFile } from 'node:fs/promises'
import { extname, resolve, sep } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'

const chromiumPath = process.env.BROWSERSKILL_TEST_CHROMIUM
const playwrightPath = process.env.BROWSERSKILL_TEST_PLAYWRIGHT
assert.ok(chromiumPath && playwrightPath, 'Set BROWSERSKILL_TEST_CHROMIUM and BROWSERSKILL_TEST_PLAYWRIGHT')
const { chromium } = await import(pathToFileURL(playwrightPath).href)
const dist = fileURLToPath(new URL('../dist', import.meta.url))
const mimeTypes = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml', '.json': 'application/json', '.wasm': 'application/wasm' }
const server = createServer(async (req, res) => {
  const url = new URL(req.url, 'http://127.0.0.1')
  const file = resolve(dist, '.' + (extname(url.pathname) ? url.pathname : '/index.html'))
  try {
    assert.ok(file.startsWith(dist + sep))
    res.setHeader('Content-Type', mimeTypes[extname(file)] || 'application/octet-stream')
    res.end(await readFile(file))
  } catch { res.writeHead(404).end() }
})
await new Promise((ready, fail) => { server.once('error', fail); server.listen(0, '127.0.0.1', ready) })
const origin = `http://127.0.0.1:${server.address().port}`

const knowledgeBase = { id: 'kb-a', name: 'Fixture Library', tenant_id: 1, type: 'document' }
const a = { id: 'policy-a', knowledge_base_id: knowledgeBase.id, tenant_id: 1, type: 'file', title: 'Fixture Policy A', file_name: 'policy-a.md', file_type: 'md', file_hash: 'a' }
const b = { id: 'policy-b', knowledge_base_id: knowledgeBase.id, tenant_id: 1, type: 'file', title: 'Fixture Policy B', file_name: 'policy-b.md', file_type: 'md', file_hash: 'b' }
const user = { id: 'fixture-user', username: 'reader-fixture', email: 'reader@fixture.invalid', tenant_id: 1, role: 'admin' }
const tenant = { id: 1, name: 'Fixture Tenant', role: 'admin' }
const docs = new Map([[a.id, a], [b.id, b]])
const annotations = { revision: 1, labels: {} }
const overview = {
  counts: { active: 2, pending: 0, scheduled: 0, stale: 0, archived: 0, superseded: 0, failed: 0 },
  documents: [...docs.values()].map(doc => ({ document_id: doc.id, title: doc.title, state: 'active', last_event_id: 'fixture-event', annotations })),
  total: 2, page: 1, page_size: 35, can_write: true, rule_revision: 1,
  rules: { rules: { effective_delay_days: 0, valid_days: 90, review_days: 30 }, revision: 1, token: 'fixture-rules' },
}
const detail = { snapshot: { current: { state: 'active', last_event_id: 'fixture-event' }, annotations, review_overdue: false }, events: [], events_truncated: false, can_write: true }
const previewRequests = []
const badWrites = []
let browser
try {
  browser = await chromium.launch({ executablePath: chromiumPath, headless: true })
  const page = await browser.newPage({ locale: 'en-US', viewport: { width: 1640, height: 950 } })
  await page.addInitScript(({ user, tenant }) => {
    localStorage.setItem('weknora_token', 'reader-fixture-token')
    localStorage.setItem('weknora_user', JSON.stringify(user))
    localStorage.setItem('weknora_tenant', JSON.stringify(tenant))
    if (!localStorage.getItem('locale')) localStorage.setItem('locale', 'en-US')
    localStorage.setItem('weknora:new-user-guide-done:v1', '1')
    localStorage.setItem('weknora:assistant-entry-guide-done:v0.9.0', '1')
  }, { user, tenant })
  await page.route('**/*', async route => {
    const request = route.request()
    const url = new URL(request.url())
    if (url.origin !== origin) return route.abort()
    const path = url.pathname
    if (!path.startsWith('/api/')) return route.continue()
    const respond = data => route.fulfill({ json: { success: true, data } })
    if (request.method() === 'PUT' || request.method() === 'DELETE') badWrites.push(path)
    if (path.endsWith('/auth/me')) return respond({ user, tenant, tenant_role: 'admin' })
    if (path.endsWith('/knowledge-bases')) return respond([knowledgeBase])
    if (path.endsWith(`/knowledge-bases/${knowledgeBase.id}`)) return respond(knowledgeBase)
    if (path.endsWith('/lifecycle')) return respond(overview)
    if (path.includes('/lifecycle/documents/')) return respond(detail)
    if (path.endsWith(`/knowledge-bases/${knowledgeBase.id}/knowledge`)) return respond([...docs.values()])
    if (path.endsWith('/knowledge/batch')) return respond(url.searchParams.getAll('ids').map(id => docs.get(id)).filter(Boolean))
    if (path.endsWith('/business/related')) return respond(url.searchParams.get('document_id') === a.id ? [
      { knowledge_base_id: knowledgeBase.id, document_id: b.id, relation_id: 'supplement', edge_type: 'supplements', direction: 'incoming', confidence: 1 },
      { knowledge_base_id: 'unavailable-kb', document_id: 'foreign', relation_id: 'foreign', edge_type: 'cites', direction: 'incoming', confidence: 1 },
    ] : [])
    const match = /\/knowledge\/([^/]+)(\/preview)?$/.exec(path)
    if (match && docs.has(match[1])) {
      const doc = docs.get(match[1])
      if (!match[2]) return respond(doc)
      previewRequests.push(doc.id)
      return route.fulfill({ status: 200, contentType: 'text/markdown', body: doc === a ? '# Evidence in pane A' : '# Evidence in pane B' })
    }
    return respond([])
  })

  await page.goto(origin + '/platform/documents?kb=kb-a')
  await page.getByRole('button', { name: a.title, exact: true }).click()
  await page.getByRole('button', { name: 'Read original', exact: true }).click()
  await page.waitForURL('**/platform/reader?**')
  await page.getByRole('heading', { name: 'Document Reader', exact: true }).waitFor()
  await page.getByText('Evidence in pane A', { exact: true }).waitFor()
  const item = page.locator('.reader-relation-list li').filter({ hasText: b.title })
  await item.getByRole('button', { name: 'Add to comparison' }).click()
  await page.getByText('Evidence in pane B', { exact: true }).waitFor()
  assert.equal(await page.getByText('Evidence in pane A', { exact: true }).count(), 1)
  assert.equal(await page.locator('.reader-panes--compare').count(), 1)
  const divider = page.getByRole('separator', { name: /comparison/i }).last()
  await divider.focus()
  await divider.press('ArrowRight')
  assert.ok(Number(await divider.getAttribute('aria-valuenow')) > 50)
  const leftPane = page.locator('.reader-pane').first()
  const widthBeforeDrag = (await leftPane.boundingBox()).width
  const handle = await divider.boundingBox()
  const x = handle.x + handle.width / 2
  const y = handle.y + handle.height / 2
  await page.mouse.move(x, y)
  await page.mouse.down()
  await page.mouse.move(x - 80, y, { steps: 4 })
  await page.mouse.up()
  assert.ok((await leftPane.boundingBox()).width < widthBeforeDrag)
  await page.locator('.reader-tab').filter({ hasText: a.title }).getByRole('button').last().click()
  await page.waitForURL('**/platform/reader')
  await page.getByRole('button', { name: 'Back to Document Workbench' }).click()
  await page.waitForURL('**/platform/documents?kb=kb-a')
  await page.locator('.setting-drawer__title').getByText(a.title, { exact: true }).waitFor()
  await page.getByRole('button', { name: 'Read original', exact: true }).waitFor()

  const count = previewRequests.length
  await page.goto(origin + '/platform/reader?kb=kb-a&doc=policy-a&version=old-version')
  await page.getByText('Only the history record is available.', { exact: false }).waitFor()
  assert.equal(previewRequests.length, count, 'historical links must not fetch latest bytes')

  await page.setViewportSize({ width: 850, height: 700 })
  await page.goto(origin + '/platform/reader?kb=kb-a&doc=policy-a&compare=policy-b')
  await page.getByText('Evidence in pane A', { exact: true }).waitFor()
  await page.getByText('B', { exact: true }).click()
  await page.getByText('Evidence in pane B', { exact: true }).waitFor()
  assert.equal(await page.getByText('Evidence in pane A', { exact: true }).count(), 0)
  await page.evaluate(() => localStorage.setItem('locale', 'zh-CN'))
  await page.reload()
  await page.getByRole('heading', { name: '文档阅读中心', exact: true }).waitFor()
  await page.getByText('Evidence in pane A', { exact: true }).waitFor()
  assert.deepEqual(badWrites, [])
  console.log('Reader browser fixture passed: workbench restoration, same-KB comparison, keyboard and pointer resizing, pinned denial, narrow A/B, en-US and zh-CN, read-only requests')
} finally {
  await browser?.close()
  server.close()
}
