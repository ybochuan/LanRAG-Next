interface CacheEntry {
  expiresAt: number
  value: unknown
}

const entries = new Map<string, CacheEntry>()
const inflight = new Map<string, Promise<unknown>>()
let generation = 0

function matchesPrefix(key: string, prefix: string): boolean {
  return key === prefix || key.startsWith(`${prefix}|`)
}

export function cacheKey(...parts: unknown[]): string {
  return parts
    .map((part) => (typeof part === 'string' ? part : JSON.stringify(part)))
    .join('|')
}

export function peekCache<T>(key: string): T | undefined {
  const entry = entries.get(key)
  if (!entry) return undefined
  if (entry.expiresAt <= Date.now()) {
    entries.delete(key)
    return undefined
  }
  return entry.value as T
}

export function invalidateCache(prefix?: string): void {
  generation += 1
  if (!prefix) {
    entries.clear()
    inflight.clear()
    return
  }
  for (const key of entries.keys()) {
    if (matchesPrefix(key, prefix)) entries.delete(key)
  }
  for (const key of inflight.keys()) {
    if (matchesPrefix(key, prefix)) inflight.delete(key)
  }
}

/**
 * Runs one loader per key while a request is in flight. A successful result is
 * kept for ttlMs so page switches can reuse it without another network call.
 */
export async function cached<T>(key: string, loader: () => Promise<T>, ttlMs = 15_000): Promise<T> {
  const hit = peekCache<T>(key)
  if (hit !== undefined) return hit

  const pending = inflight.get(key)
  if (pending) return pending as Promise<T>

  const requestGeneration = generation
  const request = loader()
    .then((value) => {
      if (requestGeneration === generation) {
        entries.set(key, { expiresAt: Date.now() + ttlMs, value })
      }
      return value
    })
    .finally(() => {
      if (inflight.get(key) === request) inflight.delete(key)
    })

  inflight.set(key, request)
  return request
}

export function clearRequestCache(): void {
  generation += 1
  entries.clear()
  inflight.clear()
}
