import { formatMoney, fromMinor, groupAmount, lineAmount, toMinor } from './money'

describe('money preview', () => {
  it('reproduces the reference invoice lines', () => {
    expect(lineAmount({ quantity: '14', unitPrice: '192.94', discountPercent: '' }, 2)).toBe(270116n)
    expect(lineAmount({ quantity: '1', unitPrice: '550', discountPercent: '0' }, 2)).toBe(55000n)
    expect(fromMinor(325116n, 2)).toBe('3,251.16')
  })
  it('rounds half away from zero like the server', () => {
    expect(lineAmount({ quantity: '1.5', unitPrice: '3.33', discountPercent: '' }, 2)).toBe(500n)
    expect(lineAmount({ quantity: '2', unitPrice: '100', discountPercent: '10' }, 2)).toBe(18000n)
  })
  it('rejects invalid input', () => {
    expect(toMinor('1.234', 2)).toBeNull()
    expect(toMinor('abc', 2)).toBeNull()
    expect(lineAmount({ quantity: 'x', unitPrice: '1', discountPercent: '' }, 2)).toBeNull()
  })
  it('handles zero-decimal currencies and negatives', () => {
    expect(toMinor('2000', 0)).toBe(2000n)
    expect(fromMinor(-123456n, 2)).toBe('-1,234.56')
    expect(formatMoney('3251.16', 'ZAR')).toBe('ZAR 3,251.16')
    expect(groupAmount('2688.00')).toBe('2,688.00')
    expect(groupAmount('-1234567')).toBe('-1,234,567')
  })
})
