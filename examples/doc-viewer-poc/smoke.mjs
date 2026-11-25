import assert from 'node:assert/strict'
import { readFile, writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import { chromium } from 'playwright-core'
import { preview } from '../../frontend/node_modules/vite/dist/node/index.js'

const artifacts = resolve('../../tmp/doc-viewer-poc')
const expected = JSON.parse(await readFile(resolve(artifacts, 'public/samples/manifest.json'), 'utf8'))
const server = await preview({ preview: { host: '127.0.0.1', port: 0, strictPort: true } })
const origin = `http://127.0.0.1:${server.httpServer.address().port}`
const base = `${origin}/doc-viewer-poc/`
const external = new Set()
const badResponses = []
const assets = new Map()
const errors = []
const workerSchemes = new Set()
const evidence = {}
let browser

async function ready(pane) {
  await pane.locator('[data-ready="true"]').waitFor({ timeout: 30_000 })
  await pane.locator('canvas:visible').first().waitFor({ timeout: 30_000 })
  await pane.page().waitForFunction(testId => {
    const target = document.querySelector(`[data-testid="${testId}"] canvas`)
    if (!target?.width || !target.height) return false
    const sample = document.createElement('canvas')
    sample.width = sample.height = 80
    const context = sample.getContext('2d')
    context.drawImage(target, 0, 0, 80, 80)
    const pixels = context.getImageData(0, 0, 80, 80).data
    let marks = 0
    for (let i = 0; i < pixels.length; i += 4) {
      if (pixels[i + 3] > 200 && Math.min(pixels[i], pixels[i + 1], pixels[i + 2]) < 180) marks++
    }
    return marks > 20
  }, await pane.getAttribute('data-testid'), { timeout: 30_000 })
}

async function search(pane, term, count = 1) {
  await pane.getByRole('textbox', { name: '页内查找' }).fill(term)
  await pane.getByRole('button', { name: '查找' }).click()
  const result = pane.locator(`[data-match-count="${count}"]`)
  await result.waitFor()
  return Number(await result.getAttribute('data-match-page')) + 1
}

try {
  browser = await chromium.launch({
    ...(process.env.DOC_VIEWER_EDGE ? { executablePath: process.env.DOC_VIEWER_EDGE } : { channel: 'msedge' }),
    args: ['--proxy-server=http://127.0.0.1:1', '--proxy-bypass-list=localhost;127.0.0.1', '--disable-background-networking'],
  })
  const context = await browser.newContext({ viewport: { width: 1720, height: 1100 }, locale: 'zh-CN' })
  context.on('request', request => {
    if (/^https?:/.test(request.url()) && !request.url().startsWith(origin + '/')) external.add(request.url())
  })
  await context.route('**/*', route => {
    const url = route.request().url()
    return url.startsWith(origin + '/') ? route.continue() : route.abort()
  })
  const page = await context.newPage()
  page.on('worker', worker => workerSchemes.add(new URL(worker.url()).protocol))
  context.on('response', response => {
    const { pathname } = new URL(response.url())
    if (response.status() >= 400) badResponses.push(`${response.status()} ${pathname}`)
    if (/\/vendor\/zrimo\/|\.wasm$|\/render-worker-host-/.test(pathname)) {
      assets.set(pathname, response.headers()['content-type'])
    }
  })
  page.on('pageerror', error => errors.push(error.message))
  await page.goto(base)
  const native = page.getByTestId('native')
  const paneA = page.getByTestId('zrimo-a')
  const paneB = page.getByTestId('zrimo-b')

  for (const [name, title, token, lastText] of [
    ['policy', '合成资料归档制度', '制度末尾核验', '第 4.06 条'],
    ['form', '合成跨页检查表', '表单末尾核验', '表格末行核验'],
  ]) {
    const start = performance.now()
    await page.getByTestId(`sample-${name}`).click()
    const [nativeMs, zrimoMs] = await Promise.all([
      native.locator('.docx-container').getByText(title, { exact: true }).waitFor({ timeout: 30_000 })
        .then(() => Math.round(performance.now() - start)),
      ready(paneA).then(() => Math.round(performance.now() - start)),
    ])
    await page.screenshot({ path: resolve(artifacts, `${name}-comparison.png`), fullPage: true })
    const matchPage = await search(paneA, token)
    const pages = Number(await paneA.locator('[data-ready="true"]').getAttribute('data-page-count'))
    assert.ok(pages > 1 && matchPage <= pages, `${name} should retain multiple navigable pages`)
    await native.locator('.docx-container').getByText(lastText, { exact: false }).first().scrollIntoViewIfNeeded()
    await page.screenshot({ path: resolve(artifacts, `${name}-end.png`), fullPage: true })
    evidence[name] = {
      nativeSections: await native.locator('.docx-container section').count(),
      zrimoPages: pages, uniqueLastMatchPage: matchPage, nativeMs, zrimoMs,
    }
  }

  await page.getByTestId('mode-dual').click()
  await Promise.all([ready(paneA), ready(paneB)])
  await search(paneB, '制度末尾核验')
  await page.screenshot({ path: resolve(artifacts, 'dual-comparison.png'), fullPage: true })
  await page.getByTestId('close-b').click()
  await paneB.waitFor({ state: 'detached' })
  await search(paneA, '表格末行核验')
  await search(paneA, '检查点 ', 42)
  for (let cycle = 0; cycle < 3; cycle++) {
    await page.getByTestId('mode-dual').click()
    await page.getByTestId('close-b').click()
    await paneB.waitFor({ state: 'detached' })
  }
  await page.getByTestId('sample-policy').click()
  await page.getByTestId('sample-form').click()
  await ready(paneA)
  await search(paneA, '表单末尾核验')

  await page.getByTestId('mode-upstream').click()
  await ready(paneA)
  assert.ok(await paneA.locator('.zrimo-ui').count(), 'expected the upstream toolbar wrapper')
  await page.getByTestId('mode-compare').click()
  await ready(paneA)
  assert.equal(await paneA.locator('.zrimo-ui').count(), 0)

  await page.getByTestId('local-file').setInputFiles({
    name: 'local-policy.docx',
    mimeType: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
    buffer: await readFile(resolve(artifacts, 'public/samples/policy.docx')),
  })
  await paneA.getByRole('status').filter({ hasText: 'local-policy.docx' }).waitFor()
  await search(paneA, '制度末尾核验')
  await native.locator('.docx-container').getByText('合成资料归档制度', { exact: true }).waitFor()

  assert.deepEqual([...external], [])
  assert.deepEqual(badResponses, [])
  assert.deepEqual(errors, [])
  const wasm = [...assets].find(([path]) => /\/docx_parser_bg-.*\.wasm$/.test(path))
  assert.match(wasm?.[1] || '', /^application\/wasm/)
  // The pinned modern Office adapter uses the main thread. A separate request
  // checks that the other formats' packaged worker files can be served as JS.
  const workerPath = 'vendor/zrimo/workers/pdf.worker.min.mjs'
  const workerFile = await context.request.get(new URL(workerPath, base).href)
  const workerMime = workerFile.headers()['content-type']
  assert.ok(workerFile.ok())
  assert.match(workerMime || '', /javascript/)
  const report = {
    browser: browser.version(), fixtures: expected, cases: evidence,
    assets: Object.fromEntries(assets), workerSchemes: [...workerSchemes],
    packagedWorkerMime: { path: workerPath, contentType: workerMime },
    external: [...external], badResponses, errors,
  }
  await writeFile(resolve(artifacts, 'smoke.json'), JSON.stringify(report, null, 2) + '\n')
  console.log(JSON.stringify(report, null, 2))
} finally {
  await browser?.close()
  await new Promise((resolveClose, rejectClose) => server.httpServer.close(error => (
    error ? rejectClose(error) : resolveClose()
  )))
}
