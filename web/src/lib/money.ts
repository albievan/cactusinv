// Display-side helpers. The server computes the real totals (ADR-005); these only
// preview what the user is typing, using integer minor units to avoid float drift.

/**
 * normalizeDecimal turns a decimal comma into a point ("192,94" → "192.94"), as an iPhone in
 * a comma region types it, when it cannot be a thousands separator: one comma, no point, and
 * not exactly three digits after it ("1,500" stays as typed and is refused as ambiguous).
 * The server applies the same rule (domain.parseFixed).
 */
export function normalizeDecimal(value: string): string {
  const v = value.trim()
  const m = /^(-?\d*),(\d*)$/.exec(v)
  return m && m[2]!.length !== 3 ? `${m[1]}.${m[2]}` : v
}

export function toMinor(value: string, minorUnits: number): bigint | null {
  const m = /^\s*(-?)(\d*)(?:\.(\d*))?\s*$/.exec(normalizeDecimal(value))
  if (!m || (m[2] === '' && (m[3] ?? '') === '')) return null
  const frac = (m[3] ?? '').padEnd(minorUnits, '0')
  if (frac.length > minorUnits) return null
  const v = BigInt((m[2] || '0') + frac)
  return m[1] === '-' ? -v : v
}

export function fromMinor(v: bigint, minorUnits: number): string {
  const neg = v < 0n
  const s = (neg ? -v : v).toString().padStart(minorUnits + 1, '0')
  const int = minorUnits ? s.slice(0, -minorUnits) : s
  const frac = minorUnits ? '.' + s.slice(-minorUnits) : ''
  return (neg ? '-' : '') + int.replace(/\B(?=(\d{3})+(?!\d))/g, ',') + frac
}

/** Round a/b half away from zero. */
function divRound(a: bigint, b: bigint): bigint {
  const neg = a < 0n !== b < 0n
  const aa = a < 0n ? -a : a
  const bb = b < 0n ? -b : b
  const q = (aa * 2n + bb) / (2n * bb)
  return neg ? -q : q
}

export interface PreviewLine {
  quantity: string
  unitPrice: string
  discountPercent: string
}

/** lineAmount mirrors domain.Line.Amount: round(qty×price) − round(gross×disc). */
export function lineAmount(l: PreviewLine, minorUnits: number): bigint | null {
  const q = toMinor(l.quantity, 3)
  const p = toMinor(l.unitPrice, minorUnits)
  const d = l.discountPercent.trim() === '' ? 0n : toMinor(l.discountPercent, 2)
  if (q === null || p === null || d === null) return null
  const gross = divRound(q * p, 1000n)
  return gross - divRound(gross * d, 10000n)
}

export function formatMoney(amount: string, currency: string): string {
  const [i = '0', f] = amount.replace('-', '').split('.')
  const grouped = i.replace(/\B(?=(\d{3})+(?!\d))/g, ',')
  return `${amount.startsWith('-') ? '-' : ''}${currency} ${grouped}${f !== undefined ? '.' + f : ''}`
}

/** groupAmount adds thousands separators to a decimal string: "2688.00" → "2,688.00". */
export function groupAmount(amount: string): string {
  const neg = amount.startsWith('-')
  const [i = '0', f] = amount.replace('-', '').split('.')
  return (neg ? '-' : '') + i.replace(/\B(?=(\d{3})+(?!\d))/g, ',') + (f !== undefined ? '.' + f : '')
}

/** sumAmounts adds decimal strings exactly in minor units: ["1.235", "2.001"] (3) → "3.236". */
export function sumAmounts(amounts: string[], minorUnits: number): string {
  let total = 0n
  for (const a of amounts) total += toMinor(a, minorUnits) ?? 0n
  return fromMinor(total, minorUnits).replace(/,/g, '')
}

/** zero is "0" in a currency's minor units: "0.00", "0" (JPY), "0.000" (KWD). */
export const zero = (minorUnits: number) => (minorUnits ? `0.${'0'.repeat(minorUnits)}` : '0')
