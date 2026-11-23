import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = readFileSync(new URL('./desktopDownload.ts', import.meta.url), 'utf8')

test('managed downloads are capped at 256 MiB and fall back without a record', () => {
  assert.match(source, /MAX_MANAGED_DOWNLOAD_BYTES = 256 \* 1024 \* 1024/)
  assert.match(source, /blob\.size > MAX_MANAGED_DOWNLOAD_BYTES \|\| !isManagedDownloadAvailable\(\)/)
  assert.match(source, /browserDownload\(blob, fileName\)/)
})

test('managed downloads require the packaged desktop native bridge', () => {
  assert.match(source, /isManagedDownloadAvailable\(\): boolean \{\s*return isPackagedDesktop\(\) && nativeBridge\(\) !== null/)
  assert.match(source, /window as Window & \{\s*go\?: \{ main\?: \{ App\?: Partial<NativeDownloadBridge> \} \}/)
})

test('download notifications are scoped and sanitized before reaching core', () => {
  assert.match(source, /link\.startsWith\('\/platform\/'\) && !link\.startsWith\('\/\/'\)/)
  assert.match(source, /replace\(\/\[\^A-Za-z0-9_-\]\/g, ''\)\.slice\(0, 36\)/)
  assert.match(source, /createDownloadNotification\(\{/)
  assert.match(source, /MarkBusinessDownloadSynced\(record\.id, data\.id\)/)
})

test('desktop save writes 2 MiB base64 chunks and aborts temp files on failure', () => {
  assert.match(source, /CHUNK_BYTES = 2 \* 1024 \* 1024/)
  assert.match(source, /WriteBusinessDownloadChunk\(session\.sessionId, toBase64\(bytes\)\)/)
  assert.match(source, /AbortBusinessDownload\(session\.sessionId\)/)
})

test('a successful managed save raises a localized toast and local unread event', () => {
  assert.match(source, /unread\?: boolean/)
  assert.match(source, /MarkBusinessDownloadsSeen: \(ids: string\[\]\) => Promise<void>/)
  assert.match(source, /emitBusinessDownloadSaved\(record\)/)
  assert.match(source, /MessagePlugin\.success\(i18n\.global\.t\('notifications\.downloadsSaved', \{ fileName: record\.fileName \}\)\)/)
  assert.match(source, /export async function markBusinessDownloadsSeen\(ids: string\[\]\): Promise<void>/)
})
