import { useEffect, useRef, useState } from 'react'

/**
 * RichNotesEditor is the invoice notes' basic word processor (M33): font, size, bold,
 * italic, underline, strike, colour, alignment, lists, tables and pictures. Its value is
 * HTML; the server keeps only what its allowlist knows (domain.CleanNotes), and that is
 * what the PDF prints. Plain notes saved before open as text. Nothing here decides what is
 * allowed; pasted content is cleaned only so a paste cannot run anything in the page.
 */

const fonts = [
  { label: 'Sans', family: 'Arial, sans-serif' },
  { label: 'Serif', family: '"Times New Roman", serif' },
  { label: 'Mono', family: '"Courier New", monospace' },
] as const
const sizes = [7, 8, 10, 12, 14, 18, 24] as const
const maxPicture = 500_000 // bytes, as domain.NoteImageMax
const maxPictureWidth = 1000 // pixels

/** isRich reports whether stored notes are formatted (HTML) rather than plain text. */
export function isRich(notes: string): boolean {
  return notes.trimStart().startsWith('<')
}

function escapeHTML(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
}

/**
 * toEditorHTML opens plain notes as text, a line per line. Formatted notes go through
 * cleanPasted too: notes saved before M33 were never cleaned by the server, and the editor
 * must never run markup it is given.
 */
export function toEditorHTML(notes: string): string {
  if (!notes) return ''
  if (isRich(notes)) return cleanPasted(notes)
  return notes.split('\n').map((l) => `<div>${l ? escapeHTML(l) : '<br>'}</div>`).join('')
}

/** fromEditorHTML is the value to save: '' for an empty editor, else HTML starting with a tag. */
export function fromEditorHTML(el: HTMLElement): string {
  if (!el.textContent?.trim() && !el.querySelector('img, table')) return ''
  const html = el.innerHTML.trim()
  return html.startsWith('<') ? html : `<div>${html}</div>`
}

const allowedTags = new Set(['P', 'DIV', 'BR', 'B', 'STRONG', 'I', 'EM', 'U', 'S', 'STRIKE', 'DEL', 'SPAN', 'FONT', 'UL', 'OL', 'LI',
  'TABLE', 'THEAD', 'TBODY', 'TFOOT', 'TR', 'TD', 'TH', 'IMG', 'H1', 'H2', 'H3', 'H4', 'H5', 'H6', 'BLOCKQUOTE'])
const droppedTags = new Set(['SCRIPT', 'STYLE', 'IFRAME', 'OBJECT', 'EMBED', 'SVG', 'MATH', 'TEMPLATE', 'NOSCRIPT', 'META', 'LINK', 'TITLE', 'FORM', 'INPUT', 'BUTTON', 'SELECT', 'TEXTAREA', 'VIDEO', 'AUDIO'])
const allowedCSS = ['color', 'font-size', 'font-family', 'font-weight', 'font-style', 'text-decoration', 'text-decoration-line', 'text-align']

/** cleanPasted keeps formatting the notes can hold and drops the rest, in an inert document. */
export function cleanPasted(html: string): string {
  const doc = new DOMParser().parseFromString(html, 'text/html')
  const walk = (n: Element) => {
    for (const c of [...n.children]) {
      if (droppedTags.has(c.tagName)) {
        c.remove()
        continue
      }
      walk(c)
      if (!allowedTags.has(c.tagName)) {
        c.replaceWith(...c.childNodes)
        continue
      }
      for (const a of [...c.attributes]) {
        const keep = (a.name === 'style') || (c.tagName === 'IMG' && (a.name === 'width' || (a.name === 'src' && /^data:image\/(png|jpe?g);base64,/i.test(a.value))))
          || (c.tagName === 'FONT' && ['face', 'size', 'color'].includes(a.name))
        if (!keep) c.removeAttribute(a.name)
      }
      if (c.tagName === 'IMG' && !c.getAttribute('src')) {
        c.remove()
        continue
      }
      const st = (c as HTMLElement).style
      if (st) {
        const kept = allowedCSS.map((p) => [p, st.getPropertyValue(p)] as const).filter(([, v]) => v && !/url\(|expression/i.test(v))
        if (kept.length) c.setAttribute('style', kept.map(([p, v]) => `${p}: ${v}`).join('; '))
        else c.removeAttribute('style')
      }
    }
  }
  walk(doc.body)
  return doc.body.innerHTML
}

/** insertHTML inserts at the caret, also where execCommand is missing (tests). */
function insertHTML(editor: HTMLElement, html: string) {
  editor.focus()
  if (typeof document.execCommand === 'function' && document.execCommand('insertHTML', false, html)) return
  const sel = window.getSelection()
  let range = sel && sel.rangeCount ? sel.getRangeAt(0) : null
  if (!range || !editor.contains(range.commonAncestorContainer)) {
    range = document.createRange()
    range.selectNodeContents(editor)
    range.collapse(false)
  }
  range.deleteContents()
  range.insertNode(range.createContextualFragment(html))
}

/** format applies an editing command (document.execCommand) to the selection. */
function format(cmd: string, value?: string) {
  if (typeof document.execCommand !== 'function') return
  document.execCommand('styleWithCSS', false, 'true')
  document.execCommand(cmd, false, value)
}

/** pictureHTML reads an image file as a PNG or JPEG data: URI of at most maxPicture bytes. */
async function pictureHTML(file: File): Promise<string> {
  if (!/^image\/(png|jpeg)$/.test(file.type)) throw new Error('Choose a PNG or JPEG picture.')
  // Read as a data: URI: the page's Content-Security-Policy allows data: images, not blob:.
  const url = await new Promise<string>((ok, fail) => {
    const r = new FileReader()
    r.onload = () => ok(String(r.result))
    r.onerror = () => fail(new Error('The picture could not be read.'))
    r.readAsDataURL(file)
  })
  const img = await new Promise<HTMLImageElement>((ok, fail) => {
    const i = new Image()
    i.onload = () => ok(i)
    i.onerror = () => fail(new Error('The picture could not be read.'))
    i.src = url
  })
  let w = Math.min(img.naturalWidth, maxPictureWidth)
  for (let attempt = 0; attempt < 6; attempt++) {
    const h = Math.round((img.naturalHeight * w) / img.naturalWidth)
    const canvas = document.createElement('canvas')
    canvas.width = w
    canvas.height = h
    canvas.getContext('2d')?.drawImage(img, 0, 0, w, h)
    const data = file.type === 'image/png' ? canvas.toDataURL('image/png') : canvas.toDataURL('image/jpeg', 0.85)
    const bytes = Math.floor(((data.length - data.indexOf(',') - 1) * 3) / 4)
    if (bytes <= maxPicture) return `<div><img src="${data}" width="${Math.min(w, 600)}"></div><div><br></div>`
    w = Math.round(w * 0.75)
  }
  throw new Error('The picture is too large, even made smaller: choose one under 500 kB.')
}

export function RichNotesEditor({ id, value, onChange, readOnly = false, minHeight = 140, label = 'Invoice notes' }: {
  id?: string
  value: string
  onChange?: (html: string) => void
  readOnly?: boolean
  minHeight?: number
  label?: string
}) {
  const ref = useRef<HTMLDivElement>(null)
  const emitted = useRef<string | null>(null)
  const fileRef = useRef<HTMLInputElement>(null)
  const [tableOpen, setTableOpen] = useState(false)
  const [rows, setRows] = useState('3')
  const [cols, setCols] = useState('3')
  const [error, setError] = useState('')
  // Where the caret was before a table's size or a picture file is chosen (focus leaves).
  const caret = useRef<Range | null>(null)
  const keepCaret = () => {
    const sel = window.getSelection()
    caret.current = sel && sel.rangeCount && ref.current?.contains(sel.getRangeAt(0).commonAncestorContainer) ? sel.getRangeAt(0).cloneRange() : null
  }
  const backToCaret = () => {
    ref.current?.focus()
    const sel = window.getSelection()
    if (caret.current && sel) {
      sel.removeAllRanges()
      sel.addRange(caret.current)
    }
  }

  // A value from outside (loading, Cancel) replaces the content; one the editor just
  // emitted does not, so the caret stays where it is.
  useEffect(() => {
    if (ref.current && value !== emitted.current) {
      ref.current.innerHTML = toEditorHTML(value)
      emitted.current = value
    }
  }, [value])

  const emit = () => {
    if (!ref.current) return
    const v = fromEditorHTML(ref.current)
    emitted.current = v
    onChange?.(v)
  }
  const run = (cmd: string, v?: string) => {
    ref.current?.focus()
    format(cmd, v)
    emit()
  }
  const setSize = (pt: number) => {
    const el = ref.current
    if (!el) return
    el.focus()
    format('fontSize', '7')
    // fontSize only knows 1–7: the largest is replaced with the size chosen.
    el.querySelectorAll<HTMLElement>('font[size="7"], span[style*="xxx-large"], span[style*="-webkit-xxx-large"]').forEach((n) => {
      n.removeAttribute('size')
      n.style.fontSize = `${pt}pt`
    })
    emit()
  }
  const insertTable = () => {
    if (!ref.current) return
    const clamp = (v: string, hi: number) => Math.min(hi, Math.max(1, Math.round(Number(v)) || 1))
    const nRows = clamp(rows, 50)
    const nCols = clamp(cols, 10)
    backToCaret()
    const head = `<tr>${Array.from({ length: nCols }, (_, i) => `<th>Heading ${i + 1}</th>`).join('')}</tr>`
    const body = Array.from({ length: Math.max(nRows - 1, 1) }, () => `<tr>${'<td><br></td>'.repeat(nCols)}</tr>`).join('')
    insertHTML(ref.current, `<table><tbody>${head}${body}</tbody></table><div><br></div>`)
    setTableOpen(false)
    emit()
  }
  const addPicture = async (file: File | undefined) => {
    if (!file || !ref.current) return
    setError('')
    try {
      const html = await pictureHTML(file)
      backToCaret()
      insertHTML(ref.current, html)
      emit()
    } catch (e) {
      setError((e as Error).message)
    }
  }

  const btn = (labelText: string, icon: string, onClick: () => void) => (
    <button type="button" className="btn btn-sm btn-outline-primary btn-icon" title={labelText} aria-label={labelText}
      onMouseDown={(e) => e.preventDefault()} onClick={onClick}>
      <i className={`bi bi-${icon}`} aria-hidden="true" />
    </button>
  )

  return (
    <div className="rich-notes">
      {!readOnly && (
        <div className="rich-notes-toolbar d-flex flex-wrap align-items-center gap-1 mb-1" role="toolbar" aria-label={`${label} formatting`}>
          <select className="form-select form-select-sm w-auto" aria-label="Font" defaultValue="" onChange={(e) => { if (e.target.value) run('fontName', e.target.value); e.target.value = '' }}>
            <option value="" disabled>Font</option>
            {fonts.map((f) => <option key={f.label} value={f.family}>{f.label}</option>)}
          </select>
          <select className="form-select form-select-sm w-auto" aria-label="Size" defaultValue="" onChange={(e) => { if (e.target.value) setSize(Number(e.target.value)); e.target.value = '' }}>
            <option value="" disabled>Size</option>
            {sizes.map((s) => <option key={s} value={s}>{s} pt{s === 8 ? ' (normal)' : ''}</option>)}
          </select>
          {btn('Bold', 'type-bold', () => run('bold'))}
          {btn('Italic', 'type-italic', () => run('italic'))}
          {btn('Underline', 'type-underline', () => run('underline'))}
          {btn('Strikethrough', 'type-strikethrough', () => run('strikeThrough'))}
          <label className="btn btn-sm btn-outline-primary btn-icon mb-0 position-relative" title="Text colour" onMouseDown={(e) => e.preventDefault()}>
            <i className="bi bi-palette" aria-hidden="true" />
            <input type="color" className="rich-notes-colour" aria-label="Text colour" defaultValue="#c0392b" onChange={(e) => run('foreColor', e.target.value)} />
          </label>
          {btn('Align left', 'text-left', () => run('justifyLeft'))}
          {btn('Centre', 'text-center', () => run('justifyCenter'))}
          {btn('Align right', 'text-right', () => run('justifyRight'))}
          {btn('Bulleted list', 'list-ul', () => run('insertUnorderedList'))}
          {btn('Numbered list', 'list-ol', () => run('insertOrderedList'))}
          {btn('Insert table', 'table', () => { keepCaret(); setTableOpen((o) => !o) })}
          {btn('Insert picture', 'image', () => { keepCaret(); fileRef.current?.click() })}
          {btn('Clear formatting', 'eraser', () => run('removeFormat'))}
          <input ref={fileRef} type="file" accept="image/png,image/jpeg" className="d-none" aria-label="Picture file"
            onChange={(e) => { void addPicture(e.target.files?.[0]); e.target.value = '' }} />
        </div>
      )}
      {tableOpen && (
        <div className="d-flex align-items-end gap-2 mb-2 small" role="group" aria-label="New table">
          <label>Rows <input type="number" className="form-control form-control-sm" min={1} max={50} value={rows} onChange={(e) => setRows(e.target.value)} /></label>
          <label>Columns <input type="number" className="form-control form-control-sm" min={1} max={10} value={cols} onChange={(e) => setCols(e.target.value)} /></label>
          <button type="button" className="btn btn-sm btn-primary" onMouseDown={(e) => e.preventDefault()} onClick={insertTable}>Add table</button>
        </div>
      )}
      {error && <div role="alert" className="small text-danger mb-1">{error}</div>}
      <div
        id={id}
        ref={ref}
        className="form-control rich-notes-content"
        style={{ minHeight }}
        contentEditable={!readOnly}
        suppressContentEditableWarning
        role="textbox"
        aria-multiline="true"
        aria-label={label}
        aria-readonly={readOnly || undefined}
        onInput={emit}
        onBlur={emit}
        onPaste={(e) => {
          const html = e.clipboardData.getData('text/html')
          const file = [...e.clipboardData.files].find((f) => /^image\/(png|jpeg)$/.test(f.type))
          if (file) {
            e.preventDefault()
            void addPicture(file)
          } else if (html && ref.current) {
            e.preventDefault()
            insertHTML(ref.current, cleanPasted(html))
            emit()
          }
        }}
        onDrop={(e) => {
          const file = [...e.dataTransfer.files].find((f) => /^image\/(png|jpeg)$/.test(f.type))
          e.preventDefault()
          if (file) void addPicture(file)
        }}
      />
    </div>
  )
}
