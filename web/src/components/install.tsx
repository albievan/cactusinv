import { useState } from 'react'

// Install commands for the cactusinv program, one per operating system and processor: shown on
// the sign-in page and in System Settings → Version. The setup scripts (setup.sh, setup.ps1 at
// the repository root) pick the right file and check it; the others fetch one particular file
// of the newest GitHub Release.

const RAW = 'https://raw.githubusercontent.com/albievan/cactusinv/main'
const LATEST = 'https://github.com/albievan/cactusinv/releases/latest/download'

export type InstallCommand = {
  label: string
  /** The GOOS/GOARCH values ("linux/amd64") it suits; an entry without "/" means any processor. */
  platforms: string[]
  command: string
}

const unixFile = (asset: string) => `curl -fL -o cactusinv ${LATEST}/${asset} && chmod +x cactusinv`
const windowsFile = (asset: string) => `Invoke-WebRequest -UseBasicParsing -OutFile cactusinv.exe ${LATEST}/${asset}`

export const setupCommands: InstallCommand[] = [
  { label: 'Linux and macOS · any processor', platforms: ['linux', 'darwin'], command: `curl -fsSL ${RAW}/setup.sh | sh` },
  { label: 'Windows (PowerShell) · any processor', platforms: ['windows'], command: `irm ${RAW}/setup.ps1 | iex` },
]

export const fileCommands: InstallCommand[] = [
  { label: 'Linux · Intel/AMD 64-bit (amd64, x86_64)', platforms: ['linux/amd64'], command: unixFile('cactusinv-linux-amd64') },
  { label: 'Linux · ARM 64-bit (arm64, aarch64: Raspberry Pi 4/5, Graviton)', platforms: ['linux/arm64'], command: unixFile('cactusinv-linux-arm64') },
  { label: 'macOS · Apple Silicon (M1–M4, darwin arm64)', platforms: ['darwin/arm64'], command: unixFile('cactusinv-darwin-arm64') },
  { label: 'macOS · Intel (darwin amd64)', platforms: ['darwin/amd64'], command: unixFile('cactusinv-darwin-amd64') },
  { label: 'Windows · Intel/AMD 64-bit (amd64, x64)', platforms: ['windows/amd64'], command: windowsFile('cactusinv-windows-amd64.exe') },
  { label: 'Windows · ARM 64-bit (arm64: Snapdragon, Surface Pro X)', platforms: ['windows/arm64'], command: windowsFile('cactusinv-windows-arm64.exe') },
]

/** suits reports whether a command is the one for platform ("linux/amd64"). */
export function suits(c: InstallCommand, platform: string): boolean {
  return !!platform && c.platforms.some((p) => p === platform || p === platform.split('/')[0])
}

/** copyText puts text on the clipboard, also where the Clipboard API is missing (plain http). */
async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    // fall through to the old way
  }
  const area = document.createElement('textarea')
  area.value = text
  area.setAttribute('readonly', '')
  area.style.position = 'fixed'
  area.style.opacity = '0'
  document.body.appendChild(area)
  area.select()
  let ok = false
  try {
    ok = document.execCommand('copy')
  } catch {
    ok = false
  }
  area.remove()
  return ok
}

function CommandRow({ c, current, dark, badge }: { c: InstallCommand; current: boolean; dark: boolean; badge: string }) {
  const [copied, setCopied] = useState<'' | 'yes' | 'no'>('')
  const copy = async () => {
    setCopied((await copyText(c.command)) ? 'yes' : 'no')
    window.setTimeout(() => setCopied(''), 2000)
  }
  const tone = dark
    ? current ? 'bg-white bg-opacity-10' : ''
    : current ? 'bg-success-subtle' : ''
  return (
    <li className={`d-flex align-items-center gap-2 rounded px-2 py-2 ${tone}`}>
      <div className="flex-grow-1" style={{ minWidth: 0 }}>
        <div className={`fw-semibold small ${dark ? 'text-white' : ''}`}>
          {c.label}
          {current && (
            <span className={`badge ms-2 border ${dark ? 'border-light text-white' : 'border-success text-success-emphasis bg-success-subtle'}`}>{badge}</span>
          )}
        </div>
        <code className={`d-block text-truncate small ${dark ? 'text-cream' : 'text-body-secondary'}`} title={c.command}>{c.command}</code>
      </div>
      <button
        type="button"
        className={`btn btn-sm flex-shrink-0 ${dark ? 'btn-outline-light' : 'btn-outline-secondary'}`}
        style={{ minWidth: 44, minHeight: 44 }}
        onClick={() => void copy()}
        aria-label={`Copy the command for ${c.label}`}
        title="Copy"
      >
        <i className={`bi ${copied === 'yes' ? 'bi-clipboard-check' : copied === 'no' ? 'bi-clipboard-x' : 'bi-clipboard'}`} aria-hidden="true" />
      </button>
      <span className="visually-hidden" role="status">{copied === 'yes' ? 'Copied' : copied === 'no' ? 'Could not copy: select the command and copy it' : ''}</span>
    </li>
  )
}

/**
 * InstallCommands lists the install command for every operating system and processor, each with
 * a copy button. platform ("linux/amd64") marks the matching rows with badge; dark styles it for
 * the sign-in page's green panel.
 */
export function InstallCommands({ platform = '', dark = false, badge = 'This server' }: { platform?: string; dark?: boolean; badge?: string }) {
  const heading = `text-uppercase fw-semibold small mb-1 ${dark ? 'text-cream opacity-75' : 'text-body-secondary'}`
  return (
    <div className="vstack gap-2">
      <div>
        <p className={heading} style={{ letterSpacing: '.04em' }}>Setup script: picks the right file itself and checks it</p>
        <ul className="list-unstyled vstack gap-1 mb-0">
          {setupCommands.map((c) => <CommandRow key={c.label} c={c} current={suits(c, platform)} dark={dark} badge={badge} />)}
        </ul>
      </div>
      <div>
        <p className={heading} style={{ letterSpacing: '.04em' }}>One particular file</p>
        <ul className="list-unstyled vstack gap-1 mb-0">
          {fileCommands.map((c) => <CommandRow key={c.label} c={c} current={suits(c, platform)} dark={dark} badge={badge} />)}
        </ul>
      </div>
    </div>
  )
}
