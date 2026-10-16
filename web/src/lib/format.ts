export function formatDate(iso?: string | null): string {
  if (!iso) return '—'
  const d = new Date(iso.length === 10 ? iso + 'T00:00:00' : iso)
  return d.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' })
}

export function formatDateTime(iso?: string | null): string {
  if (!iso) return '—'
  return new Date(iso).toLocaleString(undefined, { day: 'numeric', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' })
}

export function todayISO(): string {
  const d = new Date()
  return new Date(d.getTime() - d.getTimezoneOffset() * 60000).toISOString().slice(0, 10)
}

/** Value for <input type="datetime-local"> from an ISO instant, in local time. */
export function toLocalInput(iso: string): string {
  const d = new Date(iso)
  return new Date(d.getTime() - d.getTimezoneOffset() * 60000).toISOString().slice(0, 16)
}

/**
 * formatInZone shows an instant in the IANA time zone the server named, with the zone's
 * name: "Fri 16 Oct 2026, 09:00 UTC". The zone comes from the server (the schedule's or
 * the business time zone); nothing here decides which zone applies. An unknown zone falls
 * back to the browser's time, unlabelled.
 */
export function formatInZone(iso: string | null | undefined, timeZone: string | null | undefined): string {
  if (!iso) return '—'
  if (!timeZone) return formatDateTime(iso)
  let parts: Intl.DateTimeFormatPart[]
  try {
    parts = new Intl.DateTimeFormat('en-GB', {
      weekday: 'short', day: 'numeric', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit', hourCycle: 'h23', timeZone,
    }).formatToParts(new Date(iso))
  } catch {
    return formatDateTime(iso)
  }
  const p = (t: Intl.DateTimeFormatPartTypes) => parts.find((x) => x.type === t)?.value ?? ''
  return `${p('weekday')} ${p('day')} ${p('month')} ${p('year')}, ${p('hour')}:${p('minute')} ${timeZone}`
}
