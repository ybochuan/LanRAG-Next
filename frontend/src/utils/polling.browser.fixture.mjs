// Local-only browser fixture for P2-D visibility/polling acceptance. Production
// never imports this module. It uses the real polling helpers and records real
// HTTP requests against the fixture server so the browser test can count them.
import { createServer } from 'vite'
import { fileURLToPath } from 'node:url'

const PAGE = `<!DOCTYPE html>
<html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>P2-D polling fixture</title>
<style>body{font-family:Arial,"Microsoft YaHei",sans-serif;margin:24px}button{margin:4px}pre{background:#f5f5f5;padding:12px}</style>
<div>
  <button id="hide">hide</button><button id="show">show</button><button id="unmount">unmount</button>
  <pre id="stats">starting</pre>
</div>
<script type="module" src="/polling-entry.js"></script>
`

const ENTRY = `import {
  DOCUMENT_STATUS_POLL_BACKOFF_MS,
  MOVE_PROGRESS_POLL_BACKOFF_MS,
  WIKI_STATUS_POLL_BACKOFF_MS,
  addPageVisibilityListener,
  isPageHidden,
  pollingBackoffDelay,
} from '/src/utils/polling.ts'

const tasks = [
  { name: 'doc', normal: DOCUMENT_STATUS_POLL_BACKOFF_MS[0], schedule: DOCUMENT_STATUS_POLL_BACKOFF_MS },
  { name: 'wiki', normal: WIKI_STATUS_POLL_BACKOFF_MS[0], schedule: WIKI_STATUS_POLL_BACKOFF_MS },
  { name: 'move', normal: MOVE_PROGRESS_POLL_BACKOFF_MS[0], schedule: MOVE_PROGRESS_POLL_BACKOFF_MS },
]
const state = { hidden: false, disposed: false, timers: {}, resume: {}, failures: {}, hits: {} }
for (const task of tasks) { state.failures[task.name] = 0; state.hits[task.name] = 0; state.resume[task.name] = false }
Object.defineProperty(document, 'visibilityState', {
  configurable: true,
  get: () => state.hidden ? 'hidden' : 'visible',
})
const render = () => {
  const el = document.getElementById('stats')
  if (el) el.textContent = JSON.stringify({ hidden: state.hidden, disposed: state.disposed, hits: state.hits, failures: state.failures, timers: Object.keys(state.timers).filter(k => state.timers[k]) })
}
const clearTask = (name) => {
  if (state.timers[name]) { clearTimeout(state.timers[name]); state.timers[name] = null }
}
const schedule = (task, delay) => {
  if (state.disposed || isPageHidden()) return
  clearTask(task.name)
  state.timers[task.name] = setTimeout(async () => {
    state.timers[task.name] = null
    if (state.disposed || isPageHidden()) return
    try {
      const response = await fetch('/__fixture/hit?task=' + encodeURIComponent(task.name), { cache: 'no-store' })
      if (!response.ok) throw new Error('fixture hit failed')
      state.failures[task.name] = 0
      state.hits[task.name] += 1
    } catch {
      state.failures[task.name] = Math.min(state.failures[task.name] + 1, task.schedule.length - 1)
    }
    schedule(task, pollingBackoffDelay(state.failures[task.name], task.schedule))
    render()
  }, delay)
}
const pause = () => {
  for (const task of tasks) {
    clearTask(task.name)
    state.resume[task.name] = true
  }
}
const resume = () => {
  for (const task of tasks) {
    if (!state.resume[task.name]) continue
    state.resume[task.name] = false
    schedule(task, 0)
  }
}
const start = () => { for (const task of tasks) schedule(task, task.normal); render() }
addPageVisibilityListener(() => {
  if (isPageHidden()) pause()
  else resume()
  render()
})
window.__fixture = {
  start, pause, resume,
  hide: () => { state.hidden = true; pause(); document.dispatchEvent(new Event('visibilitychange')); render() },
  show: () => { state.hidden = false; document.dispatchEvent(new Event('visibilitychange')); render() },
  unmount: () => { state.disposed = true; pause(); render() },
  snapshot: () => ({ ...state, timers: Object.keys(state.timers).filter(k => state.timers[k]) }),
}
document.getElementById('hide').addEventListener('click', window.__fixture.hide)
document.getElementById('show').addEventListener('click', window.__fixture.show)
document.getElementById('unmount').addEventListener('click', window.__fixture.unmount)
start()
`

export async function createPollingFixture(port = 0) {
  const root = fileURLToPath(new URL('../../', import.meta.url))
  const hits = { doc: 0, wiki: 0, move: 0 }
  const server = await createServer({
    root, configFile: false, cacheDir: root + '/node_modules/.vite-polling-fixture',
    plugins: [{
      name: 'p2d-polling-fixture', enforce: 'pre',
      configureServer(server) {
        server.middlewares.use(async (req, res, next) => {
          const url = new URL(req.url, 'http://localhost')
          if (url.pathname === '/__fixture/state') {
            res.statusCode = 200; res.setHeader('Content-Type', 'application/json')
            res.end(JSON.stringify(hits)); return
          }
          if (url.pathname === '/__fixture/hit') {
            const task = url.searchParams.get('task')
            if (task && task in hits) hits[task] += 1
            res.statusCode = 200; res.setHeader('Content-Type', 'application/json')
            res.end(JSON.stringify({ ok: true, task, hits })); return
          }
          if (url.pathname === '/polling') {
            res.setHeader('Content-Type', 'text/html'); res.end(PAGE); return
          }
          if (url.pathname === '/polling-entry.js') {
            res.setHeader('Content-Type', 'application/javascript'); res.end(ENTRY); return
          }
          next()
        })
      },
    }],
    server: { host: '127.0.0.1', port, strictPort: port !== 0 },
  })
  await server.listen()
  const address = server.httpServer.address()
  return { server, url: `http://127.0.0.1:${address.port}/polling`, hits }
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  const fixture = await createPollingFixture(Number(process.env.POLLING_FIXTURE_PORT || 18993))
  process.stdout.write(fixture.url + '\n')
  const stop = async () => { await fixture.server.close(); process.exit(0) }
  process.once('SIGINT', stop); process.once('SIGTERM', stop)
}
