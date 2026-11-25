import { createRequire } from 'node:module'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import vue from '../../frontend/node_modules/@vitejs/plugin-vue/dist/index.mjs'

const root = dirname(fileURLToPath(import.meta.url))
const frontend = resolve(root, '../../frontend')
const requireFrontend = createRequire(resolve(frontend, 'package.json'))

export default {
  root,
  base: '/doc-viewer-poc/',
  publicDir: resolve(root, '../../tmp/doc-viewer-poc/public'),
  build: {
    outDir: resolve(root, '../../tmp/doc-viewer-poc/dist'),
    emptyOutDir: true,
  },
  plugins: [vue()],
  resolve: {
    alias: [
      { find: '@', replacement: resolve(frontend, 'src') },
      { find: /^vue$/, replacement: requireFrontend.resolve('vue/dist/vue.runtime.esm-bundler.js') },
      { find: /^vue-i18n$/, replacement: requireFrontend.resolve('vue-i18n/dist/vue-i18n.mjs') },
      { find: /^tdesign-vue-next$/, replacement: resolve(frontend, 'node_modules/tdesign-vue-next/es/index.mjs') },
      { find: /^tdesign-icons-vue-next$/, replacement: resolve(frontend, 'node_modules/tdesign-icons-vue-next/esm/index.js') },
      { find: '@vue-office/pptx', replacement: resolve(frontend, 'node_modules/@vue-office/pptx/lib/v3/vue-office-pptx.mjs') },
    ],
  },
}
