import { useState, type FormEvent } from 'react'
import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { useVersion } from '../lib/version'
import { useAuth } from './auth'
import { CactusMark } from './Logo'
import {
  IconBox, IconCalendar, IconShield, IconFold, IconHome, IconInvoice, IconLogout, IconMail, IconMenu, IconSearch, IconSettings, IconUnfold, IconUser, IconUsers,
} from './icons'

export const nav = [
  { to: '/', label: 'Dashboard', icon: IconHome, end: true },
  { to: '/invoices', label: 'Invoices', icon: IconInvoice },
  { to: '/customers', label: 'Customers', icon: IconUsers },
  { to: '/schedules', label: 'Schedules', icon: IconCalendar },
  { to: '/products', label: 'Products', icon: IconBox },
  { to: '/emails', label: 'Outbox', icon: IconMail },
  { to: '/audit', label: 'Audit log', icon: IconShield },
  { to: '/settings', label: 'Settings', icon: IconSettings },
]

const mobileTabs = [
  { to: '/', label: 'Dashboard', icon: IconHome, end: true },
  { to: '/invoices', label: 'Invoices', icon: IconInvoice },
  { to: '/customers', label: 'Customers', icon: IconUsers },
  { to: '/schedules', label: 'Schedules', icon: IconCalendar },
  { to: '/more', label: 'More', icon: IconMenu },
]

function readFolded(): boolean {
  try {
    return localStorage.getItem('cinv.nav.folded') === '1'
  } catch {
    return false
  }
}

export function Layout() {
  const { user, logout } = useAuth()
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
    <div className="min-h-screen md:flex">
      <aside className={`sticky top-0 hidden h-screen shrink-0 flex-col bg-gradient-to-b from-brand-800 to-brand-950 text-cream-100 md:flex ${folded ? 'w-[4.5rem]' : 'w-60'}`}>
        <div className={`px-3 pt-4 ${folded ? 'text-center' : ''}`}>
          <div className={`flex items-center gap-2 ${folded ? 'justify-center' : ''}`}>
            <CactusMark className="h-7 w-7" color="#e9dfc9" />
            {!folded && (
              <span className="text-lg font-semibold">
                cactus<span className="text-[#d9c89f]">Inv</span>
              </span>
            )}
          </div>
          {!folded && <div className="mt-1 text-xs text-cream-300/80" data-testid="version">{version}</div>}
        </div>
        {!folded && (
          <form onSubmit={search} className="mt-4 flex gap-2 px-3" role="search">
            <input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search invoices" aria-label="Search invoices" className="min-w-0 flex-1 rounded-lg border border-cream-300/30 bg-brand-900/40 px-3 py-2 text-sm text-cream-50 placeholder:text-cream-300/70 focus:border-cream-300 focus:outline-none" />
            <button className="rounded-lg border border-cream-300/30 px-2.5 text-cream-100 hover:bg-brand-700" aria-label="Search">
              <IconSearch width={16} height={16} />
            </button>
          </form>
        )}
        <nav className="mt-4 flex flex-1 flex-col gap-1 px-2" aria-label="Main">
          {nav.map((t) => (
            <NavLink
              key={t.to}
              to={t.to}
              end={t.end}
              title={folded ? t.label : undefined}
              className={({ isActive }) =>
                `flex items-center gap-3 rounded-xl px-3 py-2.5 text-[15px] font-medium transition ${folded ? 'justify-center' : ''} ${
                  isActive ? 'bg-brand-500 font-semibold text-white ring-2 ring-cream-100/90' : 'text-cream-100/90 hover:bg-brand-700/70'
                }`
              }
            >
              <t.icon />
              {!folded && t.label}
            </NavLink>
          ))}
        </nav>
        <div className="space-y-2 border-t border-cream-300/20 p-2 text-sm">
          {!folded && (
            <div className="flex items-center gap-2 px-2 py-1 text-cream-200">
              <IconUser width={18} height={18} /> Signed in as {user?.email}
            </div>
          )}
          <button onClick={logout} title="Sign out" className={`flex w-full items-center gap-3 rounded-lg border border-cream-300/30 px-3 py-2 font-semibold hover:bg-brand-700 ${folded ? 'justify-center' : ''}`}>
            <IconLogout width={18} height={18} /> {!folded && 'Sign out'}
          </button>
          <button onClick={toggle} title={folded ? 'Unfold' : 'Fold'} className={`flex w-full items-center gap-3 rounded-lg border border-cream-300/30 px-3 py-2 font-semibold hover:bg-brand-700 ${folded ? 'justify-center' : ''}`}>
            {folded ? <IconUnfold width={18} height={18} /> : <IconFold width={18} height={18} />} {!folded && 'Fold'}
          </button>
        </div>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="sticky top-0 z-30 flex items-center gap-2 bg-gradient-to-r from-brand-800 to-brand-900 px-4 py-3 text-cream-100 md:hidden">
          <CactusMark className="h-6 w-6" color="#e9dfc9" />
          <span className="font-semibold">
            cactus<span className="text-[#d9c89f]">Inv</span>
          </span>
        </header>
        <main className="flex-1">
          <Outlet />
        </main>
      </div>

      <nav aria-label="Main" className="fixed inset-x-0 bottom-0 z-40 grid grid-cols-5 border-t border-cream-300 bg-white pb-[env(safe-area-inset-bottom)] md:hidden">
        {mobileTabs.map((t) => (
          <NavLink key={t.to} to={t.to} end={'end' in t} className={({ isActive }) => `flex h-16 flex-col items-center justify-center gap-0.5 text-[11px] font-medium ${isActive ? 'text-brand-600' : 'text-slate-500'}`}>
            <t.icon width={22} height={22} />
            {t.label}
          </NavLink>
        ))}
      </nav>
    </div>
  )
}
