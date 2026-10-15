// Display-side helpers. The server computes the real totals (ADR-005); these only
// preview what the user is typing, using integer minor units to avoid float drift.

export function toMinor(value: string, minorUnits: number): bigint | null {
  const m = /^\s*(-?)(\d*)(?:\.(\d*))?\s*$/.exec(value)
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
