/**
 * uuid returns a random RFC 4122 v4 id. crypto.randomUUID exists only in secure contexts
 * (HTTPS or localhost), so a phone on the LAN over plain HTTP falls back to
 * crypto.getRandomValues, which is available everywhere.
 */
export function uuid(): string {
  // eslint-disable-next-line no-restricted-properties -- the one place it is feature-tested
  if (typeof crypto.randomUUID === 'function') return crypto.randomUUID()
  const b = crypto.getRandomValues(new Uint8Array(16))
  b[6] = (b[6]! & 0x0f) | 0x40
  b[8] = (b[8]! & 0x3f) | 0x80
  const h = [...b].map((x) => x.toString(16).padStart(2, '0')).join('')
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`
}
