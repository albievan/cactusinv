import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { cleanPasted, RichNotesEditor, safeHref, toEditorHTML } from './RichNotesEditor'

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

test('safeHref keeps web and e-mail addresses only, as the server does', () => {
  expect(safeHref('https://example.com/a?b=1')).toBe('https://example.com/a?b=1')
  expect(safeHref(' www.example.com ')).toBe('https://www.example.com')
  expect(safeHref('ann@example.com')).toBe('mailto:ann@example.com')
  expect(safeHref('MAILTO:ann@example.com')).toBe('mailto:ann@example.com')
  for (const bad of ['javascript:alert(1)', 'data:text/html,x', 'file:///etc/passwd', '/relative', '//example.com', 'https://u:p@example.com', 'https://exa mple.com', 'ftp://x.example', '']) {
    expect(safeHref(bad)).toBeNull()
  }
})

test('a link is inserted from the toolbar; pasted links keep only web and e-mail addresses', async () => {
  const values: string[] = []
  render(<Harness initial="" onValue={(v) => values.push(v)} />)
  await userEvent.click(screen.getByRole('button', { name: 'Insert link' }))
  const address = screen.getByRole('textbox', { name: 'Link address' })
  await userEvent.type(address, 'javascript:alert(1)')
  await userEvent.click(screen.getByRole('button', { name: 'Add link' }))
  expect(screen.getByRole('alert')).toHaveTextContent('Enter a web address')
  await userEvent.clear(address)
  await userEvent.type(address, 'www.cactus.example/help')
  await userEvent.click(screen.getByRole('button', { name: 'Add link' }))
  expect(values.at(-1)).toContain('<a href="https://www.cactus.example/help">https://www.cactus.example/help</a>')
  expect(screen.queryByRole('textbox', { name: 'Link address' })).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Remove link' })).toBeInTheDocument()

  const pasted = cleanPasted('<p><a href="https://example.com" target="_blank" onclick="x()">site</a> <a href="mailto:a@example.com">mail</a> <a href="vbscript:x">bad</a></p>')
  expect(pasted).toBe('<p><a href="https://example.com">site</a> <a href="mailto:a@example.com">mail</a> bad</p>')
})

test('read-only notes open a link in a new tab', async () => {
  const open = vi.spyOn(window, 'open').mockReturnValue(null)
  render(<RichNotesEditor value={'<p><a href="https://example.com/terms">terms</a></p>'} readOnly />)
  await userEvent.click(screen.getByText('terms'))
  expect(open).toHaveBeenCalledWith('https://example.com/terms', '_blank', 'noopener,noreferrer')
  open.mockRestore()
})
