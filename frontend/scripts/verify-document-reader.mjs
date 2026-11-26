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
const dist = process.env.DOC_READER_WEB_DIR
  ? resolve(process.env.DOC_READER_WEB_DIR)
  : fileURLToPath(new URL('../dist', import.meta.url))
const mimeTypes = { '.html': 'text/html', '.js': 'text/javascript', '.mjs': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml', '.json': 'application/json', '.wasm': 'application/wasm', '.woff2': 'font/woff2' }
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
const docxFixtures = process.env.DOC_READER_DOCX_FIXTURES
const formatFixtures = process.env.DOC_READER_FORMAT_FIXTURES
const complexFixtures = process.env.DOC_READER_COMPLEX_FIXTURES
const docxFiles = new Map()
const binaryFiles = new Map()
const complexFiles = new Map()
const docxType = 'application/vnd.openxmlformats-officedocument.wordprocessingml.document'
const formatFormats = ['xlsx', 'pdf', 'png', 'pptx', 'csv', 'svg', 'jpg', 'webp', 'bmp', 'tiff']
const formatTypes = {
  xlsx: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
  pptx: 'application/vnd.openxmlformats-officedocument.presentationml.presentation',
  pdf: 'application/pdf',
  png: 'image/png',
  csv: 'text/csv',
  svg: 'image/svg+xml',
  jpg: 'image/jpeg',
  webp: 'image/webp',
  bmp: 'image/bmp',
  tiff: 'image/tiff',
}
const attachmentTitle = '业务附件使用较长的合成文件名，以验证对照窗格旁边仍然能够查看完整的文档标题和核验信息'
const attachmentParent = { ...a, id: 'attachment-parent', title: '合成主件验收：验证附件的归属说明和完整标题可以在紧凑的附件面板中查看' }
if (docxFixtures) {
  docs.set(attachmentParent.id, attachmentParent)
  for (const name of ['policy', 'form']) {
    const id = `docx-${name}`
    const bytes = await readFile(resolve(docxFixtures, `${name}.docx`))
    docs.set(id, { ...a, id, title: name === 'form' ? attachmentTitle : `DOCX ${name}`, file_name: `${name}.docx`, file_type: 'docx', file_size: bytes.length })
    docxFiles.set(id, bytes)
  }
  for (const suffix of ['denied', 'large']) {
    const id = `docx-${suffix}`
    docs.set(id, { ...docs.get('docx-policy'), id, file_size: suffix === 'large' ? 50 * 1024 * 1024 + 1 : 1 })
  }
}
if (formatFixtures) {
  for (const ext of formatFormats) {
    const bytes = await readFile(resolve(formatFixtures, `policy.${ext}`))
    const id = `format-${ext}`
    docs.set(id, { ...a, id, title: `Format ${ext.toUpperCase()}`, file_name: `policy.${ext}`, file_type: ext, file_size: bytes.length })
    binaryFiles.set(id, { bytes, contentType: formatTypes[ext] })
  }
}
if (complexFixtures) {
  const complexTypes = { docx: docxType, pptx: formatTypes.pptx, pdf: formatTypes.pdf }
  for (const ext of ['docx', 'pptx', 'pdf']) {
    const bytes = await readFile(resolve(complexFixtures, `complex.${ext}`))
    const id = `complex-${ext}`
    docs.set(id, { ...a, id, title: `Complex ${ext.toUpperCase()}`, file_name: `complex.${ext}`, file_type: ext, file_size: bytes.length })
    complexFiles.set(id, { bytes, contentType: complexTypes[ext] })
  }
}
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
const chatSends = []
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
    if (request.method() === 'POST' && /\/(?:sessions|knowledge-chat|agent-chat)(?:\/|$)/.test(path)) chatSends.push(path)
    if (path.endsWith('/auth/me')) return respond({ user, tenant, tenant_role: 'admin' })
    if (path.endsWith('/knowledge-bases')) return respond([knowledgeBase])
    if (path.endsWith(`/knowledge-bases/${knowledgeBase.id}`)) return respond(knowledgeBase)
    if (path.endsWith('/lifecycle')) return respond(overview)
    if (path.includes('/lifecycle/documents/')) return respond(detail)
    if (path.endsWith(`/knowledge-bases/${knowledgeBase.id}/knowledge`)) return route.fulfill({ json: {
      success: true, page: Number(url.searchParams.get('page') || 1), page_size: Number(url.searchParams.get('page_size') || 50),
      total: docs.size, data: [...docs.values()],
    } })
    if (path.endsWith('/knowledge/batch')) return respond(url.searchParams.getAll('ids').map(id => docs.get(id)).filter(Boolean))
    if (path.endsWith('/business/attachment_list')) return respond({
      parent_id: attachmentParent.id, parent_version: attachmentParent.file_hash, parent_file_name: attachmentParent.file_name,
      items: [...docxFiles.keys()].map(id => ({ ...docs.get(id), relation_id: `attachment-${id}`, relation_type: 'attachment_of', state: 'current', revision: 1 })),
    })
    if (path.endsWith('/business/related')) return respond(url.searchParams.get('document_id') === a.id ? [
      { knowledge_base_id: knowledgeBase.id, document_id: b.id, relation_id: 'supplement', edge_type: 'supplements', direction: 'incoming', confidence: 1 },
      { knowledge_base_id: 'unavailable-kb', document_id: 'foreign', relation_id: 'foreign', edge_type: 'cites', direction: 'incoming', confidence: 1 },
    ] : [])
    const match = /\/knowledge\/([^/]+)(\/preview)?$/.exec(path)
    if (match && docs.has(match[1])) {
      const doc = docs.get(match[1])
      if (!match[2]) return respond(doc)
      previewRequests.push(doc.id)
      if (doc.id === 'docx-denied') return route.fulfill({ status: 403, json: { error: 'forbidden' } })
      if (docxFiles.has(doc.id)) {
        assert.equal(request.headers().authorization, 'Bearer reader-fixture-token')
        return route.fulfill({ status: 200, contentType: docxType, body: docxFiles.get(doc.id) })
      }
      if (complexFiles.has(doc.id)) {
        assert.equal(request.headers().authorization, 'Bearer reader-fixture-token')
        const fixture = complexFiles.get(doc.id)
        return route.fulfill({ status: 200, contentType: fixture.contentType, body: fixture.bytes })
      }
      if (binaryFiles.has(doc.id)) {
        assert.equal(request.headers().authorization, 'Bearer reader-fixture-token')
        const fixture = binaryFiles.get(doc.id)
        return route.fulfill({ status: 200, contentType: fixture.contentType, body: fixture.bytes })
      }
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

  await page.setViewportSize({ width: 748, height: 900 })
  await page.goto(origin + '/platform/reader?kb=kb-a&doc=policy-a')
  await page.getByText('Evidence in pane A', { exact: true }).waitFor()
  const pageWidth = (await page.locator('.reader-page').boundingBox()).width
  assert.ok(pageWidth >= 470 && pageWidth <= 500, 'reader content area is about 488px wide')
  const overflow = await page.evaluate(() => ['.reader-page', '.reader-picker', '.reader-search', '.reader-search__field', '.reader-search__actions', '.reader-layout', '.reader-panes']
    .filter(selector => Array.from(document.querySelectorAll(selector)).some(element => element.scrollWidth > element.clientWidth + 1)))
  assert.deepEqual(overflow, [], 'reader layout must not overflow horizontally in a 488px content area')
  if (process.env.DOC_READER_NARROW_SCREENSHOT_PATH) {
    await page.screenshot({ path: process.env.DOC_READER_NARROW_SCREENSHOT_PATH })
  }
  await page.setViewportSize({ width: 1640, height: 950 })

  if (docxFixtures) {
    await page.setViewportSize({ width: 1640, height: 950 })
    await page.evaluate(() => localStorage.setItem('locale', 'en-US'))
    const reader = origin + '/platform/reader?kb=kb-a&doc='
    const viewers = page.locator('[data-reader-engine="zrimo"]')
    function waitForPage(pane) {
      return pane.locator('.zrimo-page-status', { hasText: /Page \d+ of \d+/ }).waitFor()
    }
    await page.goto(reader + 'docx-policy&compare=docx-form')
    await Promise.all([waitForPage(viewers.nth(0)), waitForPage(viewers.nth(1))])
    assert.equal(await viewers.count(), 2)
    await viewers.nth(0).locator('canvas:visible').first().waitFor()
    await viewers.nth(1).locator('canvas:visible').first().waitFor()
    const first = viewers.nth(0)
    const second = viewers.nth(1)
    assert.equal(await viewers.getByRole('textbox', { name: 'Find in document' }).count(), 0, 'find field starts collapsed in both panes')
    await second.getByRole('button', { name: 'Find in document', exact: true }).click()
    await second.getByRole('textbox', { name: 'Find in document' }).waitFor({ timeout: 5000 })
    await second.getByRole('button', { name: 'Close search' }).click()
    await second.locator('.zrimo-viewport').click()
    assert.equal(await page.locator('.reader-pane--active').count(), 1)
    assert.equal(await second.getByRole('button', { name: /^(?:full\s*screen)(?: preview)?$/i }).count(), 1)
    await first.locator('.zrimo-viewport').click()
    assert.equal(await first.getByRole('button', { name: 'Fit page' }).count(), 1)
    await first.getByRole('button', { name: 'Fit page' }).click()
    assert.equal(await first.getByRole('button', { name: 'Fit page' }).getAttribute('aria-pressed'), 'true')
    await first.getByRole('button', { name: 'Fit width' }).click()
    const thumbnails = first.getByRole('button', { name: 'Page thumbnails', exact: true })
    await thumbnails.click({ timeout: 5000 })
    const thumbnailList = first.getByRole('region', { name: 'Page thumbnails' })
    const thirdPage = thumbnailList.getByRole('button', { name: 'Page 3', exact: true })
    await thirdPage.locator('canvas[data-ready]').waitFor({ timeout: 15000 })
    await thirdPage.click()
    await first.locator('.zrimo-page-status').filter({ hasText: /^Page 3 of / }).waitFor()
    assert.equal(await second.getByRole('region', { name: 'Page thumbnails' }).count(), 0)
    await thumbnailList.getByRole('button', { name: 'Page 1', exact: true }).click()
    await thumbnails.click()
    const spreadButton = first.getByRole('button', { name: 'Two pages', exact: true })
    await spreadButton.click()
    await first.locator('.zrimo-spread').waitFor()
    await first.locator('.zrimo-spread__page').first().waitFor()
    assert.equal(await first.locator('.zrimo-spread__page').count(), 2)
    assert.match(await first.locator('.zrimo-page-status').innerText(), /Pages 1–2 of [0-9]+/)
    if (process.env.DOC_READER_SPREAD_SCREENSHOT_PATH) {
      await page.screenshot({ path: process.env.DOC_READER_SPREAD_SCREENSHOT_PATH })
    }
    await first.getByRole('button', { name: 'Next page' }).click()
    await first.locator('.zrimo-page-status').filter({ hasText: /Pages 3–4 of [0-9]+/ }).waitFor()
    assert.equal(await first.locator('.zrimo-spread__page').count(), 2)
    await first.getByRole('button', { name: 'Previous page' }).click()
    await first.locator('.zrimo-page-status').filter({ hasText: /Pages 1–2 of [0-9]+/ }).waitFor()
    await first.getByRole('button', { name: 'Single page', exact: true }).click()
    await first.locator('.zrimo-viewport').waitFor()
    const zoomBeforePanel = Number.parseInt(await first.locator('.zrimo-zoom').textContent(), 10)
    const attachments = page.getByRole('button', { name: 'Attachments and templates', exact: true })
    await attachments.click()
    assert.equal(await attachments.getAttribute('aria-expanded'), 'true')
    const attachment = page.locator('.attachment-item').filter({ hasText: attachmentTitle })
    await attachment.waitFor()
    assert.equal(await attachment.locator('.attachment-item__title').getAttribute('title'), attachmentTitle)
    assert.equal(await attachment.locator('.attachment-item__title').evaluate(el => getComputedStyle(el).textOverflow), 'ellipsis')
    assert.equal(await attachment.locator('.attachment-item__title').evaluate(el => getComputedStyle(el).whiteSpace), 'nowrap')
    assert.equal(await attachment.getByRole('button', { name: 'Open', exact: true }).count(), 1)
    await page.waitForFunction(before => {
      const zoom = document.querySelector('.reader-pane--active .zrimo-zoom')
      return Number.parseInt(zoom?.textContent ?? '', 10) < before
    }, zoomBeforePanel)
    const viewport = await first.locator('.zrimo-viewport').boundingBox()
    const secondViewport = await second.locator('.zrimo-viewport').boundingBox()
    const pane = await page.locator('.reader-pane').first().boundingBox()
    assert.ok(viewport.y + viewport.height <= pane.y + pane.height, 'document viewport stays inside its card')
    assert.ok(Math.abs(viewport.y - secondViewport.y) < 2, 'side-by-side reading surfaces align')
    if (process.env.DOC_READER_SCREENSHOT_PATH) {
      await page.screenshot({ path: process.env.DOC_READER_SCREENSHOT_PATH })
    }
    const fullscreenButtons = viewers.getByRole('button', { name: /^(?:full\s*screen)(?: preview)?$/i })
    assert.equal(await fullscreenButtons.count(), 1, JSON.stringify(await viewers.locator('button').allTextContents()))
    const firstFullscreen = first.getByRole('button', { name: /^(?:full\s*screen)(?: preview)?$/i })
    await firstFullscreen.click()
    await first.getByRole('button', { name: /^exit full\s*screen$/i }).waitFor()
    await page.keyboard.press('Escape')
    await firstFullscreen.waitFor()
    await first.press('Control+f')
    const find = first.getByRole('textbox', { name: 'Find in document' })
    await find.fill('制度末尾核验')
    await find.press('Enter')
    await first.getByText('Match 1 of 1', { exact: true }).waitFor()
    const results = first.getByRole('region', { name: 'Search results' })
    const lastMatch = results.getByRole('button').filter({ hasText: /制度末尾核验/ })
    await lastMatch.getByText(/制度末尾核验/).waitFor()
    const destination = await lastMatch.locator('strong').textContent()
    await first.getByRole('button', { name: 'Previous page', exact: true }).click()
    assert.ok(!(await first.locator('.zrimo-page-status').textContent()).startsWith(destination + ' of '))
    await lastMatch.click()
    await first.locator('.zrimo-page-status').filter({ hasText: destination + ' of ' }).waitFor()
    await lastMatch.press('Escape')
    await find.waitFor({ state: 'detached' })
    await page.getByRole('button', { name: 'Exit comparison', exact: true }).click()
    await viewers.nth(1).waitFor({ state: 'detached' })
    await waitForPage(first)
    await attachment.getByRole('button', { name: 'Open', exact: true }).click()
    await page.waitForURL('**/platform/reader?**doc=docx-form')
    await waitForPage(first)

    await page.goto(reader + 'docx-denied')
    await page.getByText('You do not have access to perform this action.', { exact: false }).waitFor()
    assert.equal(await viewers.count(), 0)
    assert.equal(await page.locator('.docx-container').count(), 0)
    await page.goto(reader + 'docx-large')
    await page.locator('.t-alert').filter({ hasText: 'The file exceeds the 50 MiB preview limit.' }).waitFor()
    assert.ok(!previewRequests.includes('docx-large'))

    const wasmRoute = '**/*docx_parser_bg-*.wasm'
    const failWasm = route => route.abort()
    await page.route(wasmRoute, failWasm)
    await page.goto(reader + 'docx-policy')
    await page.getByText('Zrimo could not open this file. Compatibility preview is active.', { exact: true }).waitFor()
    await page.locator('.docx-container').getByText('合成资料归档制度', { exact: true }).waitFor()
    assert.equal(await viewers.count(), 0)
    await page.unroute(wasmRoute, failWasm)

    const nativeConfigRoute = route => route.fulfill({ contentType: 'text/javascript', body: 'window.__RUNTIME_CONFIG__ = { READER_DOCX_ENGINE: "native" };' })
    await page.route('**/config.js', nativeConfigRoute)
    await page.goto(reader + 'docx-form')
    await page.locator('.docx-container').getByText('合成跨页检查表', { exact: true }).waitFor()
    assert.equal(await viewers.count(), 0)
    await page.unroute('**/config.js', nativeConfigRoute)
    await page.evaluate(() => {
      const settings = JSON.parse(localStorage.getItem('WeKnora_settings') || '{}')
      settings.selectedKnowledgeBases = ['unrelated-kb']
      settings.selectedFiles = ['unrelated-file']
      settings.selectedFileKbMap = { 'unrelated-file': 'unrelated-kb' }
      localStorage.setItem('WeKnora_settings', JSON.stringify(settings))
    })
    await page.goto(origin + '/platform/reader?kb=kb-a&doc=docx-policy')
    await waitForPage(first)
    const textLayer = first.locator('[data-zrimo-layer="text"] span').filter({ hasText: /\S/ }).first()
    await textLayer.waitFor()
    const selectText = () => textLayer.evaluate(span => {
      const range = document.createRange()
      range.selectNodeContents(span)
      const selection = window.getSelection()
      selection.removeAllRanges()
      selection.addRange(range)
      return selection.toString()
    })
    const askSelection = first.getByRole('button', { name: 'Ask about selection' })
    await selectText()
    await askSelection.waitFor()
    await page.evaluate(() => window.getSelection().removeAllRanges())
    await askSelection.waitFor({ state: 'detached' })
    const selectedText = await selectText()
    await askSelection.click({ timeout: 5000 })
    await page.waitForURL('**/platform/creatChat')
    const draft = await page.locator('textarea:visible').first().inputValue()
    assert.match(draft, /DOCX policy/)
    assert.match(draft, /page 1/i)
    assert.ok(selectedText && !draft.includes(selectedText), 'the raw selection stays in the reader')
    const settings = await page.evaluate(() => JSON.parse(localStorage.getItem('WeKnora_settings')))
    assert.deepEqual(settings.selectedKnowledgeBases, [])
    assert.deepEqual(settings.selectedFiles, ['docx-policy'])
    assert.deepEqual(settings.selectedFileKbMap, { 'docx-policy': 'kb-a' })
    assert.deepEqual(chatSends, [], 'opening the selection draft does not send it')
    console.log('Reader DOCX fixture passed: thumbnails, search results and shortcuts, selection draft, active pane, fit page, bounded viewport, compact attachments, fullscreen, denied and bounded reads, fallback and rollback')
  }
  if (formatFixtures) {
    await page.setViewportSize({ width: 1640, height: 950 })
    await page.evaluate(() => localStorage.setItem('locale', 'en-US'))
    const reader = origin + '/platform/reader?kb=kb-a&doc='
    for (const ext of formatFormats) {
      await page.goto(reader + 'format-' + ext)
      const viewer = page.locator('[data-reader-engine="zrimo"]')
      await viewer.waitFor({ timeout: 15000 }).catch(async error => {
        console.log('FORMAT ' + ext + ' pane text:', await page.locator('.reader-pane').first().innerText())
        console.log('FORMAT ' + ext + ' alerts:', await page.locator('.t-alert').allTextContents())
        throw error
      })
      await viewer.locator('.zrimo-page-status').waitFor({ timeout: 15000 })
      if (ext === 'pptx' || ext === 'pdf') {
        assert.match(await viewer.locator('.zrimo-page-status').innerText(), /1 of 2/)
        const scrollable = await viewer.locator('[data-zrimo="viewport"]').evaluate(el => el.scrollHeight > el.clientHeight + 20)
        assert.ok(scrollable, ext + ' should render multiple pages in a scrollable continuous surface')
        await viewer.getByRole('button', { name: 'Two pages', exact: true }).click()
        await viewer.locator('.zrimo-spread__page').first().waitFor({ timeout: 15000 })
        assert.equal(await viewer.locator('.zrimo-spread__page').count(), 2)
        assert.match(await viewer.locator('.zrimo-page-status').innerText(), /Pages 1–2 of 2/)
        await viewer.getByRole('button', { name: 'Single page', exact: true }).click()
        await viewer.locator('.zrimo-viewport').waitFor()
      }
      await viewer.locator('.zrimo-viewport canvas:visible, .zrimo-viewport img:visible').first().waitFor({ timeout: 30000 })
      assert.equal(await page.locator('.t-alert').filter({ hasText: 'Zrimo could not open this file' }).count(), 0)
      if (ext === 'xlsx') {
        const surface = viewer.locator('[data-zrimo="spreadsheet-viewport"]')
        const viewport = await surface.boundingBox()
        await surface.click({ position: { x: viewport.width / 2, y: viewport.height / 3 } })
        await viewer.getByRole('button', { name: 'Ask about selection' }).click({ timeout: 10000 })
        await page.waitForURL('**/platform/creatChat')
        const draft = await page.locator('textarea:visible').first().inputValue()
        assert.match(draft, /Format XLSX/)
        assert.match(draft, /· [A-Z]+\d+/)
        assert.deepEqual(chatSends, [], 'opening the spreadsheet selection draft does not send it')
      }
    }
    console.log('Reader multi-format fixture passed: ' + formatFormats.map(ext => ext.toUpperCase()).join(', '))
    await page.goto(origin + '/platform/reader?kb=kb-a&doc=format-pdf&compare=format-pptx')
    const dualViewers = page.locator('[data-reader-engine="zrimo"]')
    await dualViewers.nth(1).waitFor({ timeout: 15000 })
    assert.equal(await dualViewers.count(), 2)
    for (let index = 0; index < 2; index++) {
      await dualViewers.nth(index).locator('.zrimo-page-status').waitFor({ timeout: 15000 })
      await dualViewers.nth(index).locator('.zrimo-viewport canvas:visible, .zrimo-viewport img:visible').first().waitFor({ timeout: 30000 })
    }
    console.log('Reader dual-pane multi-format fixture passed: PDF + PPTX')
  }
  if (complexFixtures) {
    await page.setViewportSize({ width: 1640, height: 950 })
    await page.evaluate(() => localStorage.setItem('locale', 'en-US'))
    const reader = origin + '/platform/reader?kb=kb-a&doc='
    const cdp = await page.context().newCDPSession(page)
    await cdp.send('Performance.enable')
    const readMetrics = async () => {
      const result = await cdp.send('Performance.getMetrics')
      const value = name => result.metrics.find(item => item.name === name)?.value ?? 0
      return { heap: value('JSHeapUsedSize'), nodes: value('Nodes') }
    }
    const samplePage = async action => {
      let peak = { heap: 0, nodes: 0 }
      let closed = false
      const timer = setInterval(() => {
        if (closed) return
        readMetrics().then(metrics => {
          peak.heap = Math.max(peak.heap, metrics.heap)
          peak.nodes = Math.max(peak.nodes, metrics.nodes)
        }).catch(() => {})
      }, 200)
      try {
        await action()
      } finally {
        closed = true
        clearInterval(timer)
      }
      const final = await readMetrics()
      return { peak, final }
    }
    const singleMetrics = {}
    for (const ext of ['docx', 'pptx', 'pdf']) {
      const started = Date.now()
      let loadedPageCount = 0
      const metrics = await samplePage(async () => {
        await page.goto(reader + 'complex-' + ext)
        const viewer = page.locator('[data-reader-engine="zrimo"]')
        await viewer.waitFor({ timeout: 20000 })
        const status = viewer.locator('.zrimo-page-status')
        await status.waitFor({ timeout: 20000 })
        const statusText = await status.innerText()
        const match = statusText.match(/Page 1 of (\d+)/)
        assert.ok(match, ext + ' should expose a page count, got ' + statusText)
        const pageCount = Number(match[1])
        assert.ok(pageCount >= (ext === 'pptx' ? 20 : 30), ext + ' should render all fixture pages, got ' + pageCount)
        const scrollable = await viewer.locator('[data-zrimo="viewport"]').evaluate(el => el.scrollHeight > el.clientHeight + 100)
        assert.ok(scrollable, ext + ' complex document should use a scrollable continuous surface')
        await viewer.locator('.zrimo-viewport canvas:visible, .zrimo-viewport img:visible').first().waitFor({ timeout: 30000 })
        loadedPageCount = pageCount
        await page.waitForTimeout(600)
      })
      singleMetrics[ext] = metrics
      console.log('Reader complex ' + ext + ' passed: ' + loadedPageCount + ' pages, continuous surface, peakHeap=' + Math.round(metrics.peak.heap / 1024 / 1024) + 'MiB, finalHeap=' + Math.round(metrics.final.heap / 1024 / 1024) + 'MiB, peakNodes=' + metrics.peak.nodes + ', loadMs=' + (Date.now() - started))
    }
    const dualMetrics = await samplePage(async () => {
      await page.goto(origin + '/platform/reader?kb=kb-a&doc=complex-pdf&compare=complex-pptx')
      const dualViewers = page.locator('[data-reader-engine="zrimo"]')
      await dualViewers.nth(1).waitFor({ timeout: 20000 })
      assert.equal(await dualViewers.count(), 2)
      for (let index = 0; index < 2; index++) {
        await dualViewers.nth(index).locator('.zrimo-page-status').waitFor({ timeout: 20000 })
        await dualViewers.nth(index).locator('.zrimo-viewport canvas:visible, .zrimo-viewport img:visible').first().waitFor({ timeout: 30000 })
      }
      await page.waitForTimeout(1000)
    })
    console.log('Reader complex dual-pane passed: PDF + PPTX, peakHeap=' + Math.round(dualMetrics.peak.heap / 1024 / 1024) + 'MiB, finalHeap=' + Math.round(dualMetrics.final.heap / 1024 / 1024) + 'MiB, peakNodes=' + dualMetrics.peak.nodes + ', singlePdfPeak=' + Math.round(singleMetrics.pdf.peak.heap / 1024 / 1024) + 'MiB')
  }
  assert.deepEqual(badWrites, [])
  console.log('Reader browser fixture passed: workbench restoration, same-KB comparison, keyboard and pointer resizing, pinned denial, narrow A/B, narrow 488px no overflow, en-US and zh-CN, read-only requests')
} finally {
  await browser?.close()
  server.close()
}
