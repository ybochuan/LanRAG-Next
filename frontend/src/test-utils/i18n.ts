import zhCN from '../i18n/locales/zh-CN'

function lookup(key: string): unknown {
  return key.split('.').reduce<unknown>((node, part) => {
    if (node && typeof node === 'object') return (node as Record<string, unknown>)[part]
    return undefined
  }, zhCN)
}

/** Resolve a locale key against the Chinese catalogue for component tests. */
export function translate(key: string, params?: Record<string, unknown> | unknown[]): string {
  const value = lookup(key)
  if (typeof value !== 'string') return key
  return value.replace(/\{(\w+)\}/g, (_, name: string) => {
    if (Array.isArray(params)) return name in params ? String(params[Number(name)]) : '{' + name + '}'
    return params && name in params ? String(params[name]) : '{' + name + '}'
  })
}
