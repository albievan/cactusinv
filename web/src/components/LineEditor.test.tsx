import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { LineEditor, newLine, type EditLine } from './LineEditor'

function Harness() {
  const [lines, setLines] = useState<EditLine[]>([newLine()])
  return <LineEditor lines={lines} onChange={setLines} currency="ZAR" minorUnits={2} />
}

afterEach(() => vi.unstubAllGlobals())

test('lines come from a product search across all products; no free text', async () => {
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({
    total: 2,
    items: [
      { id: 'p1', sku: 'SS-USERS', name: 'Stock system users', description: 'Stock system usage', unit: 'user', active: true, version: 1, prices: [{ currency: 'ZAR', unitPrice: '192.94' }] },
      { id: 'p2', sku: 'USD-ONLY', name: 'Dollar thing', description: '', unit: '', active: true, version: 1, prices: [{ currency: 'USD', unitPrice: '10.00' }] },
    ],
  }), { headers: { 'Content-Type': 'application/json' } })))
  render(<Harness />)
  expect(screen.getByText('Choose a product for every line')).toBeInTheDocument()
  const box = screen.getByRole('combobox', { name: 'Product for line 1' })
  await userEvent.type(box, 'st')
  const list = await screen.findByRole('listbox')
  expect(await within(list).findByText('No ZAR price')).toBeInTheDocument()
  await userEvent.click(within(list).getByText('Dollar thing'))
  expect(screen.getByText('Choose a product for every line')).toBeInTheDocument() // not selectable
  await userEvent.click(within(list).getByText('Stock system users'))
  await userEvent.clear(screen.getByLabelText('Quantity for line 1'))
  await userEvent.type(screen.getByLabelText('Quantity for line 1'), '14')
  expect(screen.getByText('ZAR 2,701.16')).toBeInTheDocument()
  expect(screen.queryByRole('textbox', { name: /description/i })).toBeNull()
})
