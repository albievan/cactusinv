import { useId, useState, type ReactNode } from 'react'
import { containerPlatformOf, containerPlatforms, containerSteps, hostOSLabels, type ContainerPlatform, type HostOS, type Step } from './containerSteps'
import { upgradeMethods, upgradeSteps, type UpgradeMethod } from './upgradeSteps'

// How to install and upgrade cactusInv, shown on the sign-in page and in System Settings →
// Version, in three tabs. Standalone: the install command for the cactusinv program, one per
// operating system and processor; the setup scripts (setup.sh, setup.ps1 at the repository root)
// pick the right file and check it, the others fetch one particular file of the newest GitHub
// Release. Container: step by step with deploy.sh on Docker, Podman, k3s or Kubernetes
// (containerSteps.ts). Upgrade: step by step to a newer version, per way it was installed
// (upgradeSteps.ts).

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

/** CopyButton copies text and says so (to screen readers too). */
function CopyButton({ text, label, dark }: { text: string; label: string; dark: boolean }) {
  const [copied, setCopied] = useState<'' | 'yes' | 'no'>('')
  const copy = async () => {
    setCopied((await copyText(text)) ? 'yes' : 'no')
    window.setTimeout(() => setCopied(''), 2000)
  }
  return (
    <>
      <button
        type="button"
        className={`btn btn-sm flex-shrink-0 ${dark ? 'btn-outline-light' : 'btn-outline-secondary'}`}
        style={{ minWidth: 44, minHeight: 44 }}
        onClick={() => void copy()}
        aria-label={label}
        title="Copy"
      >
        <i className={`bi ${copied === 'yes' ? 'bi-clipboard-check' : copied === 'no' ? 'bi-clipboard-x' : 'bi-clipboard'}`} aria-hidden="true" />
      </button>
      <span className="visually-hidden" role="status">{copied === 'yes' ? 'Copied' : copied === 'no' ? 'Could not copy: select the command and copy it' : ''}</span>
    </>
  )
}

function CommandRow({ c, current, dark, badge }: { c: InstallCommand; current: boolean; dark: boolean; badge: string }) {
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
      <CopyButton text={c.command} label={`Copy the command for ${c.label}`} dark={dark} />
    </li>
  )
}

const headingClass = (dark: boolean) => `text-uppercase fw-semibold small mb-1 ${dark ? 'text-cream opacity-75' : 'text-body-secondary'}`

/** StandaloneCommands lists the install command for every operating system and processor. */
function StandaloneCommands({ platform, dark, badge }: { platform: string; dark: boolean; badge: string }) {
  const heading = headingClass(dark)
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

/** guessHostOS is the operating system of the browser's computer (Linux when unknown). */
function guessHostOS(): HostOS {
  const ua = typeof navigator === 'undefined' ? '' : navigator.userAgent
  if (/Windows/.test(ua)) return 'windows'
  if (/Macintosh|Mac OS X/.test(ua) && !/iPhone|iPad/.test(ua)) return 'macos'
  return 'linux'
}

/** Choice is a row of radio buttons styled as buttons. */
function Choice<T extends string>({ legend, options, value, onChange, dark }: {
  legend: string; options: { key: T; label: string }[]; value: T; onChange: (v: T) => void; dark: boolean
}) {
  const name = useId()
  return (
    <fieldset>
      <legend className={`${headingClass(dark)} fs-6`} style={{ letterSpacing: '.04em' }}>{legend}</legend>
      <div className="d-flex flex-wrap gap-2">
        {options.map((o) => (
          <span key={o.key}>
            <input type="radio" className="btn-check" name={name} id={`${name}-${o.key}`} checked={value === o.key} onChange={() => onChange(o.key)} />
            <label className={`btn btn-sm ${dark ? 'btn-outline-light' : 'btn-outline-primary'}`} style={{ minHeight: 36 }} htmlFor={`${name}-${o.key}`}>{o.label}</label>
          </span>
        ))}
      </div>
    </fieldset>
  )
}

function StepItem({ step, n, dark }: { step: Step; n: number; dark: boolean }) {
  return (
    <li className="d-flex gap-2">
      <span
        className={`flex-shrink-0 rounded-circle d-inline-flex align-items-center justify-content-center fw-semibold small ${dark ? 'bg-white bg-opacity-25 text-white' : 'bg-primary-subtle text-primary-emphasis'}`}
        style={{ width: 28, height: 28 }}
        aria-hidden="true"
      >
        {n}
      </span>
      <div className="flex-grow-1 vstack gap-1" style={{ minWidth: 0 }}>
        <div className={`fw-semibold small ${dark ? 'text-white' : ''}`}>{step.title}</div>
        {step.text && <p className={`small mb-0 ${dark ? 'text-cream' : 'text-body-secondary'}`}>{step.text}</p>}
        {step.commands?.map((c) => (
          <div key={c.command} className={`d-flex align-items-start gap-2 rounded px-2 py-1 ${dark ? 'bg-black bg-opacity-25' : 'bg-body-secondary'}`}>
            <div className="flex-grow-1" style={{ minWidth: 0 }}>
              {c.label && <div className={`small fw-medium ${dark ? 'text-white' : ''}`}>{c.label}</div>}
              <pre className={`small mb-0 py-1 ${dark ? 'text-cream' : ''}`} style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}><code>{c.command}</code></pre>
            </div>
            <CopyButton text={c.command} label={`Copy: ${step.title}${c.label ? ` (${c.label})` : ''}`} dark={dark} />
          </div>
        ))}
      </div>
    </li>
  )
}

/** ContainerGuide is the step-by-step container install for a platform and operating system. */
function ContainerGuide({ initial, dark }: { initial: ContainerPlatform | ''; dark: boolean }) {
  const [platform, setPlatform] = useState<ContainerPlatform>(initial || 'docker')
  const [os, setOS] = useState<HostOS>(guessHostOS)
  const supported = containerPlatforms.find((p) => p.key === platform)?.os ?? ['linux']
  const shownOS = supported.includes(os) ? os : 'linux'
  const steps = containerSteps(platform, shownOS)
  return (
    <div className="vstack gap-3">
      <p className={`small mb-0 ${dark ? 'text-cream' : 'text-body-secondary'}`}>
        deploy.sh installs cactusInv with its MariaDB database as containers, from a copy of the source: on Docker or Podman on one
        computer, or on k3s or any Kubernetes cluster. It builds the image, generates the secrets, takes backups and upgrades.
      </p>
      <Choice legend="Container platform" options={containerPlatforms.map((p) => ({ key: p.key, label: p.label }))} value={platform} onChange={setPlatform} dark={dark} />
      <Choice legend="Run the steps on" options={supported.map((o) => ({ key: o, label: hostOSLabels[o] }))} value={shownOS} onChange={setOS} dark={dark} />
      <ol className="list-unstyled vstack gap-3 mb-0" aria-label={`Steps: ${containerPlatforms.find((p) => p.key === platform)?.label ?? ''} on ${hostOSLabels[shownOS]}`}>
        {steps.map((s, i) => <StepItem key={s.title} step={s} n={i + 1} dark={dark} />)}
      </ol>
    </div>
  )
}

/** UpgradeGuide is the step-by-step upgrade for the way cactusInv was installed and an operating system. */
function UpgradeGuide({ initial, dark }: { initial: UpgradeMethod; dark: boolean }) {
  const [method, setMethod] = useState<UpgradeMethod>(initial)
  const [os, setOS] = useState<HostOS>(guessHostOS)
  const m = upgradeMethods.find((u) => u.key === method) ?? upgradeMethods[0]!
  const shownOS = m.os.includes(os) ? os : 'linux'
  return (
    <div className="vstack gap-3">
      <p className={`small mb-0 ${dark ? 'text-cream' : 'text-body-secondary'}`}>
        Upgrading keeps your data, settings and users, and takes a backup first. Choose how this cactusInv was installed.
      </p>
      <Choice legend="Installed as" options={upgradeMethods.map((u) => ({ key: u.key, label: u.label }))} value={method} onChange={setMethod} dark={dark} />
      <Choice legend="Run the steps on" options={m.os.map((o) => ({ key: o, label: hostOSLabels[o] }))} value={shownOS} onChange={setOS} dark={dark} />
      <ol className="list-unstyled vstack gap-3 mb-0" aria-label={`Upgrade steps: ${m.label} on ${hostOSLabels[shownOS]}`}>
        {upgradeSteps(method, shownOS).map((s, i) => <StepItem key={s.title} step={s} n={i + 1} dark={dark} />)}
      </ol>
    </div>
  )
}

type InstallTab = 'standalone' | 'container' | 'upgrade'

const installTabs: { key: InstallTab; label: string; icon: string }[] = [
  { key: 'standalone', label: 'Standalone', icon: 'bi-download' },
  { key: 'container', label: 'Container', icon: 'bi-box-seam' },
  { key: 'upgrade', label: 'Upgrade', icon: 'bi-arrow-up-circle' },
]

/**
 * InstallCommands shows how to install and upgrade cactusInv in three tabs: Standalone (the
 * program, a command per operating system and processor, each with a copy button), Container
 * (step by step with deploy.sh) and Upgrade (step by step to a newer version). platform
 * ("linux/amd64") marks the matching standalone rows with badge; target (dockerdesktop, podman,
 * k3s, k8s) opens the Container tab at that platform and Upgrade at that way of installing;
 * standaloneIntro goes above the standalone commands; dark styles it for the sign-in page's green
 * panel. The tabs are buttons with fixed colours: the selected one is filled, so it reads in
 * light and dark mode alike.
 */
export function InstallCommands({ platform = '', dark = false, badge = 'This server', target = '', standaloneIntro }: {
  platform?: string; dark?: boolean; badge?: string; target?: string; standaloneIntro?: ReactNode
}) {
  const initial = containerPlatformOf(target)
  const [tab, setTab] = useState<InstallTab>(initial ? 'container' : 'standalone')
  const id = useId()
  return (
    <div className="vstack gap-3">
      <div className="d-flex flex-wrap gap-2" role="tablist" aria-label="Install or upgrade">
        {installTabs.map((t) => (
          <button
            key={t.key}
            type="button"
            role="tab"
            id={`${id}-${t.key}-tab`}
            aria-controls={`${id}-${t.key}`}
            aria-selected={tab === t.key}
            className={`btn fw-semibold d-inline-flex align-items-center gap-2 px-3 ${dark ? 'btn-outline-light' : 'btn-outline-primary'} ${tab === t.key ? 'active' : ''}`}
            style={{ minHeight: 44 }}
            onClick={() => setTab(t.key)}
          >
            <i className={`bi ${t.icon}`} aria-hidden="true" />
            {t.label}
          </button>
        ))}
      </div>
      <div role="tabpanel" id={`${id}-${tab}`} aria-labelledby={`${id}-${tab}-tab`} className="vstack gap-2">
        {tab === 'standalone' ? (
          <>
            {standaloneIntro}
            <StandaloneCommands platform={platform} dark={dark} badge={badge} />
          </>
        ) : tab === 'container' ? (
          <ContainerGuide initial={initial} dark={dark} />
        ) : (
          <UpgradeGuide initial={initial || 'standalone'} dark={dark} />
        )}
      </div>
    </div>
  )
}
