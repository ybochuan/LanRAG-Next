/**
 * Desktop shell injects the Core origin as `window.__WEKNORA_API_BASE__`
 * (usually `http://127.0.0.1:<port>/api/v1`). Uploads must target that origin
 * directly: Wails v2 routes page requests through WebView2's
 * WebResourceRequested handler, which drops the bytes of multipart file parts.
 * See Wails v2 issue #3037 and WebView2Feedback #2162.
 */
export function desktopOriginFromApiBase(value: unknown): string | null {
  if (typeof value !== 'string') return null;
  const trimmed = value.trim();
  if (!trimmed) return null;
  try {
    const parsed = new URL(trimmed);
    if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') return null;
    return parsed.origin;
  } catch {
    return null;
  }
}

export function getDesktopApiOrigin(): string | null {
  if (typeof window === 'undefined') return null;
  const value = (window as Window & { __WEKNORA_API_BASE__?: unknown }).__WEKNORA_API_BASE__;
  return desktopOriginFromApiBase(value);
}
