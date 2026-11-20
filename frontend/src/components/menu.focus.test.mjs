import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { pathToFileURL } from 'node:url'
import { compile } from '@vue/compiler-dom'
import { parse, compileTemplate } from '@vue/compiler-sfc'

const source = readFileSync(new URL('./menu.vue', import.meta.url), 'utf8')
const { descriptor } = parse(source)
const require = createRequire(import.meta.url)
const chromiumPath = process.env.BROWSERSKILL_TEST_CHROMIUM
const playwrightPath = process.env.BROWSERSKILL_TEST_PLAYWRIGHT

test('the sidebar template compiles', () => {
  const result = compileTemplate({ source: descriptor.template.content, filename: 'menu.vue', id: 'menu' })
  assert.deepEqual(result.errors, [])
})

test('Tab reaches the sidebar toggle, and Enter and Space keep it focused', { skip: !chromiumPath || !playwrightPath }, async t => {
  const header = descriptor.template.ast.children[0].children.find(node =>
    node.props?.some(prop => prop.name === 'class' && prop.value?.content === 'logo_row'))
  assert.ok(header)
  const { code } = compile(header.loc.source, { prefixIdentifiers: true })
  const { chromium } = await import(pathToFileURL(playwrightPath).href)
  const browser = await chromium.launch({ executablePath: chromiumPath })
  t.after(() => browser.close())
  const page = await browser.newPage({ viewport: { width: 390, height: 800 } })
  await page.setContent('<div id="app"></div>')
  await page.addScriptTag({ path: require.resolve('vue/dist/vue.global.prod.js') })
  await page.addScriptTag({ path: require.resolve('tdesign-vue-next/dist/tdesign.min.js') })
  await page.evaluate(renderCode => {
    const { computed, createApp, ref } = window.Vue
    const sidebarCollapsed = ref(true)
    const app = createApp({
      setup: () => ({
        sidebarCollapsed,
        sidebarToggleLabel: computed(() => sidebarCollapsed.value ? 'Expand sidebar' : 'Collapse sidebar'),
        toggleSidebar: () => { sidebarCollapsed.value = !sidebarCollapsed.value },
        assistantStore: { entry: 'off' },
        isLiteEdition: false,
        getImgSrc: () => '',
        t: key => key,
      }),
      render: new Function('Vue', renderCode)(window.Vue),
    })
    app.component('t-button', window.TDesign.Button)
    app.component('t-tooltip', window.TDesign.Tooltip)
    app.mount('#app')
  }, code)

  const toggle = page.locator('button[aria-expanded]')
  await page.keyboard.press('Tab')
  const button = await toggle.elementHandle()
  assert.equal(await button.evaluate(element => document.activeElement === element), true)
  assert.equal(await toggle.getAttribute('aria-label'), 'Expand sidebar')
  assert.equal(await toggle.getAttribute('aria-expanded'), 'false')

  for (const [key, expanded, label] of [['Enter', 'true', 'Collapse sidebar'], ['Space', 'false', 'Expand sidebar']]) {
    await page.keyboard.press(key)
    assert.equal(await toggle.getAttribute('aria-expanded'), expanded)
    assert.equal(await toggle.getAttribute('aria-label'), label)
    assert.equal(await button.evaluate(element => document.activeElement === element), true)
  }
})
