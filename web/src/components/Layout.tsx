import { useState, type FormEvent } from 'react'
import { Link, NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { api, unwrap } from '../lib/api/client'
import { settingsHref, useSettingsNav } from '../features/settings/settingsNav'
import { useVersion } from '../lib/version'
import { useAuth, useCan } from './auth'
import { CactusMark } from './Logo'
import {
  IconBox, IconCalendar, IconShield, IconStatement, IconFold, IconHome, IconInvoice, IconLogout, IconMail, IconMenu, IconQuote, IconSearch, IconSettings, IconUnfold, IconUser, IconUsers,
} from './icons'

/** The main menu; `need` is the permission that shows an entry (ADR-012). */
export const nav = [
  { to: '/', label: 'Dashboard', icon: IconHome, end: true, need: 'dashboard.view' },
  { to: '/invoices', label: 'Invoices', icon: IconInvoice, need: 'invoices.view' },
  { to: '/quotes', label: 'Quotes', icon: IconQuote, need: 'quotes.view' },
  { to: '/customers', label: 'Customers', icon: IconUsers, need: 'customers.view' },
  { to: '/statements', label: 'Statements', icon: IconStatement, need: 'statements.view' },
  { to: '/schedules', label: 'Schedules', icon: IconCalendar, need: 'schedules.view' },
  { to: '/products', label: 'Products', icon: IconBox, need: 'products.view' },
  { to: '/emails', label: 'Outbox', icon: IconMail, need: 'emails.view' },
  { to: '/audit', label: 'Audit log', icon: IconShield, need: 'audit.view' },
  { to: '/settings', label: 'Settings', icon: IconSettings },
]

const mobileTabs = [
  { to: '/', label: 'Dashboard', icon: IconHome, end: true, need: 'dashboard.view' },
  { to: '/invoices', label: 'Invoices', icon: IconInvoice, need: 'invoices.view' },
  { to: '/customers', label: 'Customers', icon: IconUsers, need: 'customers.view' },
  { to: '/schedules', label: 'Schedules', icon: IconCalendar, need: 'schedules.view' },
  { to: '/more', label: 'More', icon: IconMenu },
]

/** useNav is the main menu without the entries the signed-in user may not open. */
export function useNav() {
  const can = useCan()
  return nav.filter((t) => !t.need || can(t.need))
}


/**
 * SettingsNavItem is the Settings entry of the sidebar. On a settings route it expands to
 * its sub-pages (settingsNav); the sub-page shown is aria-current="page" and Settings
 * itself is marked as the current section.
 */
function SettingsNavItem({ folded }: { folded: boolean }) {
  const { pathname } = useLocation()
  const settingsNav = useSettingsNav()
  const open = pathname === '/settings' || pathname.startsWith('/settings/')
  return (
    <div>
      <NavLink
        to="/settings"
        title={folded ? 'Settings' : undefined}
        aria-label={folded ? 'Settings' : undefined}
        aria-current={open ? 'true' : undefined}
        className={`app-nav-link${folded ? ' justify-content-center' : ''}${open ? ' section' : ''}`}
      >
        <IconSettings />
        {!folded && 'Settings'}
      </NavLink>
      {open && (
        <ul aria-label="Settings" className={`list-unstyled vstack gap-1 mt-1 mb-0${folded ? '' : ' app-subnav'}`}>
          {settingsNav.map((s) => (
            <li key={s.path}>
              <NavLink
                to={settingsHref(s)}
                title={folded ? s.label : undefined}
                aria-label={folded ? s.label : undefined}
                className={({ isActive }) => `app-nav-link sub${folded ? ' justify-content-center' : ''}${isActive ? ' active' : ''}`}
              >
                <s.icon width={folded ? 18 : 16} height={folded ? 18 : 16} />
                {!folded && s.label}
              </NavLink>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

/**
 * PausedBanner is shown on every page while automatic e-mails and schedules are held after
 * a restore: restored outbox rows and schedules could repeat what was already sent, so an
 * administrator checks them and resumes.
 */
function PausedBanner() {
  const { user, refresh } = useAuth()
  const can = useCan()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  if (!user?.automationPaused) return null
  const resume = async () => {
    setBusy(true)
    setError('')
    try {
      await unwrap(api.POST('/automation/resume'))
      await refresh()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  return (
    <div role="status" className="alert alert-warning rounded-0 border-0 border-bottom border-warning-subtle d-flex flex-wrap align-items-center gap-3 mb-0 px-3 py-3 small">
      <span className="min-w-0 flex-grow-1">
        <strong>Automatic sending is paused after a restore.</strong> Check the <Link className="alert-link text-decoration-underline" to="/emails">Outbox</Link> and{' '}
        <Link className="alert-link text-decoration-underline" to="/schedules">schedules</Link>, then resume.
        {!can('administer') && ' An administrator resumes it.'}
        {error && <span className="ms-2 fw-semibold text-danger">{error}</span>}
      </span>
      {can('administer') && (
        <button className="btn btn-primary" onClick={resume} disabled={busy}>
          Resume automatic sending
        </button>
      )}
    </div>
  )
}

function readFolded(): boolean {
  try {
    return localStorage.getItem('cinv.nav.folded') === '1'
  } catch {
    return false
  }
}

export function Layout() {
  const { user, logout } = useAuth()
  const can = useCan()
  const menu = useNav()
  const tabs = mobileTabs.filter((t) => !t.need || can(t.need))
  const version = useVersion()
  const navigate = useNavigate()
  const [folded, setFolded] = useState(readFolded)
  const [q, setQ] = useState('')
  const toggle = () => {
    setFolded((f) => {
      try {
        localStorage.setItem('cinv.nav.folded', f ? '0' : '1')
      } catch {
        /* storage unavailable */
      }
      return !f
    })
  }
  const search = (e: FormEvent) => {
    e.preventDefault()
    navigate(`/invoices?q=${encodeURIComponent(q.trim())}`)
  }
  return (
    <div className="min-vh-100 d-md-flex">
      <aside className={`app-sidebar d-none d-md-flex flex-column${folded ? ' folded' : ''}`}>
        <div className={`px-3 pt-3${folded ? ' text-center' : ''}`}>
          {/* The version sits beside the mark, under the name, so the mark never runs into it. */}
          <div className={`d-flex align-items-center gap-2${folded ? ' justify-content-center' : ''}`}>
            <CactusMark className="flex-shrink-0" size={32} color="#e9dfc9" />
            {!folded && (
              <div className="min-w-0 lh-sm">
                <span className="d-block fs-5 fw-semibold">
                  cactus<span className="text-gold">Inv</span>
                </span>
                <span className="d-block text-truncate fs-8 text-cream-muted" data-testid="version">
                  {version}
                </span>
              </div>
            )}
          </div>
        </div>
        {!folded && can('invoices.view') && (
          <form onSubmit={search} className="d-flex gap-2 mt-3 px-3" role="search">
            <input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search invoices" aria-label="Search invoices" className="form-control form-control-sm app-search min-w-0" />
            <button className="btn btn-sm btn-outline-light app-sidebar-btn btn-icon" aria-label="Search" title="Search">
              <IconSearch width={16} height={16} />
            </button>
          </form>
        )}
        <nav className="d-flex flex-column flex-grow-1 gap-1 overflow-y-auto mt-2 px-3 py-1" aria-label="Main">
          {menu.map((t) =>
            t.to === '/settings' ? (
              <SettingsNavItem key={t.to} folded={folded} />
            ) : (
              <NavLink
                key={t.to}
                to={t.to}
                end={t.end}
                title={folded ? t.label : undefined}
                aria-label={folded ? t.label : undefined}
                className={({ isActive }) => `app-nav-link${folded ? ' justify-content-center' : ''}${isActive ? ' active' : ''}`}
              >
                <t.icon />
                {!folded && t.label}
              </NavLink>
            ),
          )}
        </nav>
        <div className="vstack gap-2 border-top border-light border-opacity-25 p-2 small">
          {!folded && (
            <div className="d-flex align-items-center gap-2 px-2 py-1 text-cream text-break">
              <IconUser width={18} height={18} /> Signed in as {user?.email}
            </div>
          )}
          <button onClick={logout} title="Sign out" aria-label={folded ? 'Sign out' : undefined} className={`btn btn-sm app-sidebar-btn border w-100${folded ? '' : ' justify-content-start'}`}>
            <IconLogout width={18} height={18} /> {!folded && 'Sign out'}
          </button>
          <button onClick={toggle} title={folded ? 'Unfold' : 'Fold'} aria-label={folded ? 'Unfold' : undefined} className={`btn btn-sm app-sidebar-btn border w-100${folded ? '' : ' justify-content-start'}`}>
            {folded ? <IconUnfold width={18} height={18} /> : <IconFold width={18} height={18} />} {!folded && 'Fold'}
          </button>
        </div>
      </aside>

      <div className="d-flex flex-column flex-grow-1 min-w-0">
        <header className="app-topbar sticky-top d-flex d-md-none align-items-center gap-2 px-3 py-2">
          <CactusMark size={24} color="#e9dfc9" />
          <span className="fw-semibold">
            cactus<span className="text-gold">Inv</span>
          </span>
        </header>
        <PausedBanner />
        <main className="flex-grow-1">
          <Outlet />
        </main>
      </div>

      <nav aria-label="Main" className="app-tabs fixed-bottom d-md-none bg-white border-top" style={{ gridTemplateColumns: `repeat(${tabs.length}, minmax(0, 1fr))` }}>
        {tabs.map((t) => (
          <NavLink key={t.to} to={t.to} end={'end' in t} className={({ isActive }) => `app-tab${isActive ? ' active' : ''}`}>
            <t.icon width={22} height={22} />
            {t.label}
          </NavLink>
        ))}
      </nav>
    </div>
  )
}
