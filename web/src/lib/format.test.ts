import { formatInZone } from './format'

describe('formatInZone', () => {
  test.each([
    ['2026-10-16T09:00:00Z', 'UTC', 'Fri 16 Oct 2026, 09:00 UTC'],
    // The same 09:00 UTC run, whatever the browser's zone: London shows its own clock and says so.
    ['2026-10-16T09:00:00Z', 'Europe/London', 'Fri 16 Oct 2026, 10:00 Europe/London'],
    ['2026-10-30T09:00:00Z', 'Europe/London', 'Fri 30 Oct 2026, 09:00 Europe/London'],
    ['2026-10-16T07:00:00Z', 'Africa/Johannesburg', 'Fri 16 Oct 2026, 09:00 Africa/Johannesburg'],
  ])('%s in %s', (iso, zone, want) => {
    expect(formatInZone(iso, zone)).toBe(want)
  })

  test('no instant, no zone or an unknown zone', () => {
    expect(formatInZone(undefined, 'UTC')).toBe('—')
    expect(formatInZone('2026-10-16T09:00:00Z', undefined)).not.toContain('UTC')
    expect(formatInZone('2026-10-16T09:00:00Z', 'Mars/Olympus')).not.toContain('Mars')
  })
})
