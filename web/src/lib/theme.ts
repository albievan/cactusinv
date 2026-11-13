/**
 * followSystemTheme makes the app light or dark as the device is (prefers-color-scheme),
 * and follows it when that changes; there is no toggle. Bootstrap switches on the
 * data-bs-theme attribute of <html> (an inline script would be blocked by the CSP, so
 * this runs first thing in main.tsx). It returns a function that stops following.
 */
export function followSystemTheme(root: HTMLElement = document.documentElement): () => void {
  const dark = typeof window.matchMedia === 'function' ? window.matchMedia('(prefers-color-scheme: dark)') : undefined
  // (index.html colours the browser bar per mode with two theme-color metas.)
  const apply = () => root.setAttribute('data-bs-theme', dark?.matches ? 'dark' : 'light')
  apply()
  dark?.addEventListener('change', apply)
  return () => dark?.removeEventListener('change', apply)
}
