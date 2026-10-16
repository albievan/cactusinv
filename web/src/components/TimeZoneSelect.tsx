// A pick-list of IANA time zones, so a zone is chosen rather than typed (a typo or the UTC
// default silently moved scheduled runs). The device's own zone is listed first.

export function deviceZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
  } catch {
    return 'UTC'
  }
}

function allZones(): string[] {
  try {
    const zs = (Intl as { supportedValuesOf?: (k: string) => string[] }).supportedValuesOf?.('timeZone') ?? []
    return zs.includes('UTC') ? zs : ['UTC', ...zs]
  } catch {
    return ['UTC']
  }
}

export function TimeZoneSelect({ id, value, onChange }: { id: string; value: string; onChange: (zone: string) => void }) {
  const device = deviceZone()
  const rest = allZones().filter((z) => z !== device && z !== value)
  const top = [...new Set([value, device].filter(Boolean))]
  return (
    <select id={id} className="input" value={value} onChange={(e) => onChange(e.target.value)}>
      {top.map((z) => <option key={z} value={z}>{z === device ? `${z} (this device)` : z}</option>)}
      {rest.map((z) => <option key={z} value={z}>{z}</option>)}
    </select>
  )
}
