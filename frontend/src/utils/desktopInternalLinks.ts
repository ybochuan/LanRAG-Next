import type { Router } from 'vue-router'

type DesktopWindow = Window & { runtime?: unknown }

/**
 * Wails opens native anchors through the system browser. Keep same-origin
 * application links inside the desktop router; external links and downloads
 * retain their native behavior.
 */
export function installDesktopInternalLinkGuard(router: Router) {
  if (typeof window === 'undefined' || !(window as DesktopWindow).runtime) return
  document.addEventListener('click', (event: MouseEvent) => {
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
    const target = event.target
    if (!(target instanceof Element)) return
    const anchor = target.closest('a[href]')
    if (!(anchor instanceof HTMLAnchorElement) || anchor.hasAttribute('download')) return
    const raw = anchor.getAttribute('href')
    if (!raw || raw.startsWith('#') || /^(?:javascript|mailto|tel|blob|data):/i.test(raw)) return
    let url: URL
    try {
      url = new URL(anchor.href, window.location.href)
    } catch {
      return
    }
    if (url.origin !== window.location.origin) return
    const route = router.resolve(`${url.pathname}${url.search}${url.hash}`)
    if (!route.matched.length) return
    event.preventDefault()
    void router.push(route.fullPath)
  }, true)
}
