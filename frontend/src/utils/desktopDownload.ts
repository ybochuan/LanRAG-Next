import { MessagePlugin } from 'tdesign-vue-next'
import i18n from '@/i18n'
import { createDownloadNotification, type DownloadNotificationSourceType } from '@/api/automation'
import { isPackagedDesktop } from '@/api/modelSupervisor'

/** Files up to this size go through the app-managed save path. */
export const MAX_MANAGED_DOWNLOAD_BYTES = 256 * 1024 * 1024
const CHUNK_BYTES = 2 * 1024 * 1024

export interface BusinessDownloadMeta {
  fileName: string
  sourceType: DownloadNotificationSourceType
  sourceId: string
  sourceTitle: string
  link?: string
  /** Defaults to the blob size; callers never need to compute it. */
  size?: number
}

export interface BusinessDownloadSession {
  sessionId: string
  fileName: string
  canceled: boolean
  fallback: boolean
}

export interface BusinessDownloadRecord {
  id: string
  fileName: string
  size: number
  sourceType: DownloadNotificationSourceType
  sourceId: string
  sourceTitle: string
  link?: string
  createdAt: string
  /** Local unread marker; old records without this field are treated as read. */
  unread?: boolean
  syncState: 'pending' | 'synced' | 'failed' | string
  syncError?: string
  notificationId?: string
}

type NativeDownloadBridge = {
  BeginBusinessDownload: (meta: BusinessDownloadMeta) => Promise<BusinessDownloadSession>
  WriteBusinessDownloadChunk: (sessionId: string, chunkBase64: string) => Promise<void>
  FinishBusinessDownload: (sessionId: string) => Promise<BusinessDownloadRecord>
  AbortBusinessDownload: (sessionId: string) => Promise<void>
  ListBusinessDownloads: () => Promise<BusinessDownloadRecord[]>
  MarkBusinessDownloadSynced: (id: string, notificationId: string) => Promise<void>
  MarkBusinessDownloadFailed: (id: string, message: string) => Promise<void>
  MarkBusinessDownloadsSeen: (ids: string[]) => Promise<void>
  OpenBusinessDownload: (id: string) => Promise<void>
  RevealBusinessDownload: (id: string) => Promise<void>
}

type BusinessDownloadSavedListener = (record: BusinessDownloadRecord) => void
const businessDownloadSavedListeners = new Set<BusinessDownloadSavedListener>()

/** Subscribes to records that were just saved locally; returns an unsubscribe function. */
export function onBusinessDownloadSaved(listener: BusinessDownloadSavedListener): () => void {
  businessDownloadSavedListeners.add(listener)
  return () => { businessDownloadSavedListeners.delete(listener) }
}

function emitBusinessDownloadSaved(record: BusinessDownloadRecord): void {
  for (const listener of businessDownloadSavedListeners) {
    try { listener(record) } catch { /* One listener must not block the others. */ }
  }
}

function nativeBridge(): NativeDownloadBridge | null {
  const candidate = (window as Window & {
    go?: { main?: { App?: Partial<NativeDownloadBridge> } }
  }).go?.main?.App
  return candidate?.BeginBusinessDownload
    ? candidate as NativeDownloadBridge
    : null
}

/** True only inside the packaged desktop shell with the native bridge present. */
export function isManagedDownloadAvailable(): boolean {
  return isPackagedDesktop() && nativeBridge() !== null
}

/** Browser fallback for web, oversized files, and a native save fallback. */
export function browserDownload(blob: Blob, fileName: string): void {
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = fileName || 'download'
  anchor.style.display = 'none'
  document.body.appendChild(anchor)
  anchor.click()
  anchor.remove()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

function toBase64(bytes: Uint8Array): string {
  let binary = ''
  const step = 0x8000
  for (let index = 0; index < bytes.length; index += step) {
    binary += String.fromCharCode(...bytes.subarray(index, index + step))
  }
  return btoa(binary)
}

function safeSourceId(value: string, fallback: string): string {
  const cleaned = String(value || '').replace(/[^A-Za-z0-9_-]/g, '').slice(0, 36)
  return cleaned || fallback
}

function safeLink(value: string | undefined): string {
  const link = String(value || '')
  return link.startsWith('/platform/') && !link.startsWith('//') ? link.slice(0, 512) : ''
}

async function notifyDownload(
  native: NativeDownloadBridge,
  record: BusinessDownloadRecord,
): Promise<void> {
  try {
    const { data } = await createDownloadNotification({
      file_name: record.fileName,
      source_type: record.sourceType,
      source_id: safeSourceId(record.sourceId, record.id),
      source_title: String(record.sourceTitle || record.fileName).slice(0, 255),
      link: safeLink(record.link),
      download_id: record.id,
      file_size: Math.max(0, Math.min(record.size, MAX_MANAGED_DOWNLOAD_BYTES)),
    })
    await native.MarkBusinessDownloadSynced(record.id, data.id)
    record.syncState = 'synced'
    record.syncError = ''
    record.notificationId = data.id
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error)
    record.syncState = 'failed'
    record.syncError = message
    try {
      await native.MarkBusinessDownloadFailed(record.id, message)
    } catch {
      // The local record is still shown; a later retry can resync it.
    }
  }
}

export type SaveBusinessDownloadResult =
  | { mode: 'managed'; record: BusinessDownloadRecord }
  | { mode: 'browser' }
  | { mode: 'canceled' }

/**
 * Saves a business document, attachment, or artifact through the desktop app.
 * Web and oversized downloads keep the existing browser path and create no
 * notification record.
 */
export async function saveBusinessDownload(
  blob: Blob,
  meta: BusinessDownloadMeta,
): Promise<SaveBusinessDownloadResult> {
  const fileName = meta.fileName || 'download'
  if (blob.size > MAX_MANAGED_DOWNLOAD_BYTES || !isManagedDownloadAvailable()) {
    browserDownload(blob, fileName)
    return { mode: 'browser' }
  }
  const native = nativeBridge()
  if (!native) {
    browserDownload(blob, fileName)
    return { mode: 'browser' }
  }
  const session = await native.BeginBusinessDownload({ ...meta, fileName, size: blob.size })
  if (session.canceled) return { mode: 'canceled' }
  if (session.fallback || !session.sessionId) {
    browserDownload(blob, fileName)
    return { mode: 'browser' }
  }
  try {
    for (let offset = 0; offset < blob.size; offset += CHUNK_BYTES) {
      const bytes = new Uint8Array(await blob.slice(offset, offset + CHUNK_BYTES).arrayBuffer())
      await native.WriteBusinessDownloadChunk(session.sessionId, toBase64(bytes))
    }
    const record = await native.FinishBusinessDownload(session.sessionId)
    emitBusinessDownloadSaved(record)
    MessagePlugin.success(i18n.global.t('notifications.downloadsSaved', { fileName: record.fileName }))
    await notifyDownload(native, record)
    return { mode: 'managed', record }
  } catch (error) {
    try {
      await native.AbortBusinessDownload(session.sessionId)
    } catch {
      // Abort is best effort; the session is not reused after an error.
    }
    throw error
  }
}

/** Retries local records that did not reach the core notification service. */
export async function syncPendingBusinessDownloads(): Promise<BusinessDownloadRecord[]> {
  const native = nativeBridge()
  if (!native) return []
  let records: BusinessDownloadRecord[] = []
  try {
    records = await native.ListBusinessDownloads()
  } catch {
    return []
  }
  for (const record of records) {
    if (record.syncState === 'synced') continue
    await notifyDownload(native, record)
  }
  return records
}

/** Marks local download records as read after the notification panel shows them. */
export async function markBusinessDownloadsSeen(ids: string[]): Promise<void> {
  if (!ids.length) return
  const native = nativeBridge()
  if (!native || typeof native.MarkBusinessDownloadsSeen !== 'function') return
  await native.MarkBusinessDownloadsSeen(ids)
}

export async function listBusinessDownloads(): Promise<BusinessDownloadRecord[]> {
  const native = nativeBridge()
  if (!native) return []
  try {
    return await native.ListBusinessDownloads()
  } catch {
    return []
  }
}

export async function openBusinessDownload(id: string): Promise<void> {
  const native = nativeBridge()
  if (!native) throw new Error('desktop bridge is unavailable')
  await native.OpenBusinessDownload(id)
}

export async function revealBusinessDownload(id: string): Promise<void> {
  const native = nativeBridge()
  if (!native) throw new Error('desktop bridge is unavailable')
  await native.RevealBusinessDownload(id)
}

export function formatDownloadSize(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit += 1
  }
  return `${value >= 10 || unit === 0 ? value.toFixed(0) : value.toFixed(1)} ${units[unit]}`
}
