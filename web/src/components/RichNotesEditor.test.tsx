import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { cleanPasted, RichNotesEditor, toEditorHTML } from './RichNotesEditor'

function Harness({ initial, onValue }: { initial: string; onValue: (v: string) => void }) {
  const [v, setV] = useState(initial)
  return <RichNotesEditor value={v} onChange={(x) => { setV(x); onValue(x) }} />
}

test('plain notes open as text, a line per line, and the toolbar inserts a table', async () => {
  const values: string[] = []
  render(<Harness initial={'Hours for {period}.\nThanks'} onValue={(v) => values.push(v)} />)
  const box = screen.getByRole('textbox', { name: 'Invoice notes' })
  expect(box.innerHTML).toBe('<div>Hours for {period}.</div><div>Thanks</div>')
  for (const name of ['Bold', 'Italic', 'Underline', 'Strikethrough', 'Text colour', 'Centre', 'Bulleted list', 'Insert table', 'Insert picture', 'Clear formatting']) {
    expect(screen.getAllByLabelText(name).length).toBeGreaterThan(0)
  }
  expect(screen.getByRole('combobox', { name: 'Font' })).toBeInTheDocument()
  expect(screen.getByRole('combobox', { name: 'Size' })).toBeInTheDocument()

  await userEvent.click(screen.getByRole('button', { name: 'Insert table' }))
  await userEvent.clear(screen.getByRole('spinbutton', { name: 'Rows' }))
  await userEvent.type(screen.getByRole('spinbutton', { name: 'Rows' }), '2')
  await userEvent.clear(screen.getByRole('spinbutton', { name: 'Columns' }))
  await userEvent.type(screen.getByRole('spinbutton', { name: 'Columns' }), '2')
  await userEvent.click(screen.getByRole('button', { name: 'Add table' }))
  const last = values.at(-1) ?? ''
  expect(last).toContain('<table><tbody><tr><th>Heading 1</th><th>Heading 2</th></tr><tr><td><br></td><td><br></td></tr></tbody></table>')
  expect(box.querySelectorAll('table tr')).toHaveLength(2)
})

test('notes and pastes never bring markup that runs into the page', () => {
  const dirty = '<div onclick="alert(1)">Hi<script>alert(2)</script><img src="x" onerror="alert(3)"><img src="data:image/png;base64,AAAA" onload="x()" width="20"><a href="javascript:alert(4)">link</a>' +
    '<span style="color: red; background: url(javascript:alert(5))">red</span><iframe src="https://example.test"></iframe></div>'
  const clean = cleanPasted(dirty)
  for (const bad of ['onclick', 'script', 'onerror', 'onload', 'javascript', 'iframe', 'href', 'url(', 'src="x"']) {
    expect(clean).not.toContain(bad)
  }
  expect(clean).toContain('<img src="data:image/png;base64,AAAA" width="20">')
  expect(clean).toContain('<span style="color: red">red</span>')
  expect(toEditorHTML(dirty)).toBe(clean)
  expect(toEditorHTML('a < b & c')).toBe('<div>a &lt; b &amp; c</div>')
})
