export const DOCUMENT_STATUS_POLL_BACKOFF_MS = [
  1500, 3000, 6000, 12000, 30000, 60000,
] as const

export const WIKI_STATUS_POLL_BACKOFF_MS = [
  5000, 10000, 20000, 60000,
] as const

export const MOVE_PROGRESS_POLL_BACKOFF_MS = [
  2000, 5000, 10000, 30000,
] as const

/**
 * Return the delay before the next poll after a number of consecutive
 * failures. Index 0 is the normal interval; subsequent entries are the
 * increasing retry intervals. A successful poll resets the counter to 0.
 */
export function pollingBackoffDelay(
  consecutiveFailures: number,
  schedule: readonly number[],
): number {
  if (schedule.length === 0) return 0
  const index = Math.min(Math.max(consecutiveFailures, 0), schedule.length - 1)
  return schedule[index]
}

/** Whether the current document/page is hidden from the user. */
export function isPageHidden(): boolean {
  return typeof document !== 'undefined' && document.visibilityState === 'hidden'
}

/**
 * Register a visibilitychange listener and return a disposer. The no-op
 * fallback keeps unit-test/SSR environments safe when no document exists.
 */
export function addPageVisibilityListener(listener: () => void): () => void {
  if (typeof document === 'undefined' || typeof document.addEventListener !== 'function') {
    return () => {}
  }
  document.addEventListener('visibilitychange', listener)
  return () => document.removeEventListener('visibilitychange', listener)
}
