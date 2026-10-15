import { uuid } from './uuid'

test('uuid works without crypto.randomUUID (plain HTTP on a phone)', () => {
  // eslint-disable-next-line no-restricted-properties -- saved to simulate plain HTTP
  const orig = crypto.randomUUID
  Object.defineProperty(crypto, 'randomUUID', { value: undefined, configurable: true })
  try {
    const a = uuid()
    expect(a).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)
    expect(uuid()).not.toBe(a)
  } finally {
    Object.defineProperty(crypto, 'randomUUID', { value: orig, configurable: true })
  }
})
