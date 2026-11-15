// Formatting for the Database & containers page and the dashboard's health (M57).

/** formatBytes prints a byte count for people (1024-based). */
export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let v = n / 1024
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(1)} ${units[i]}`
}

/** formatUptime prints a duration in seconds as days, hours and minutes. */
export function formatUptime(seconds: number): string {
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (d > 0) return `${d}d ${h}h`
  if (h > 0) return `${h}h ${m}m`
  return `${m}m`
}

/** formatMs prints milliseconds: one decimal below 10 ms. */
export function formatMs(ms: number): string {
  return ms < 10 ? `${ms.toFixed(1)} ms` : `${Math.round(ms)} ms`
}

export const formatCount = (n: number) => n.toLocaleString()

/** stateBadge is the badge class and icon for a service or health state. */
export function stateBadge(state: string): { cls: string; icon: string; label: string } {
  switch (state) {
    case 'running':
    case 'ok':
      return { cls: 'bg-success-subtle text-success-emphasis border-success-subtle', icon: 'bi-check-circle-fill', label: state === 'ok' ? 'OK' : 'Running' }
    case 'warning':
      return { cls: 'bg-warning-subtle text-warning-emphasis border-warning-subtle', icon: 'bi-exclamation-triangle-fill', label: 'Warning' }
    case 'unreachable':
    case 'failed':
      return { cls: 'bg-danger-subtle text-danger-emphasis border-danger-subtle', icon: 'bi-x-circle-fill', label: state === 'failed' ? 'Failed' : 'Unreachable' }
    default:
      return { cls: 'bg-secondary-subtle text-secondary-emphasis border-secondary-subtle', icon: 'bi-dash-circle', label: 'Not configured' }
  }
}
