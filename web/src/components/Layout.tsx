import { NavLink, Outlet } from 'react-router-dom'
import { useAuth } from './auth'

const tabs = [
  { to: '/', label: 'Home', icon: '⌂', end: true },
  { to: '/invoices', label: 'Invoices', icon: '🧾' },
  { to: '/schedules', label: 'Schedules', icon: '⏱' },
  { to: '/customers', label: 'Customers', icon: '👥' },
  { to: '/more', label: 'More', icon: '☰' },
]

const desktopExtra = [
  { to: '/products', label: 'Products' },
  { to: '/emails', label: 'Outbox' },
  { to: '/settings', label: 'Settings' },
]

export function Layout() {
  const { user, logout } = useAuth()
  return (
    <div className="min-h-screen md:flex">
      <aside className="hidden w-60 shrink-0 flex-col border-r border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900 md:flex">
        <div className="mb-6 flex items-center gap-2 text-lg font-bold text-brand-600">
          <img src="/favicon.svg" alt="" className="h-8 w-8" /> cactusInv
        </div>
        <nav className="flex flex-1 flex-col gap-1" aria-label="Main">
          {[...tabs.filter((t) => t.to !== '/more'), ...desktopExtra].map((t) => (
            <NavLink
              key={t.to}
              to={t.to}
              end={'end' in t}
              className={({ isActive }) => `rounded-xl px-3 py-2 text-sm font-medium ${isActive ? 'bg-brand-50 text-brand-700 dark:bg-slate-800 dark:text-brand-200' : 'hover:bg-slate-100 dark:hover:bg-slate-800'}`}
            >
              {t.label}
            </NavLink>
          ))}
        </nav>
        <div className="text-xs text-slate-500">
          {user?.email} ·{' '}
          <button className="underline" onClick={logout}>
            Sign out
          </button>
        </div>
      </aside>
      <main className="flex-1">
        <Outlet />
      </main>
      <nav
        aria-label="Main"
        className="fixed inset-x-0 bottom-0 z-40 grid grid-cols-5 border-t border-slate-200 bg-white pb-[env(safe-area-inset-bottom)] dark:border-slate-800 dark:bg-slate-900 md:hidden"
      >
        {tabs.map((t) => (
          <NavLink
            key={t.to}
            to={t.to}
            end={t.end}
            className={({ isActive }) => `flex h-16 flex-col items-center justify-center text-[11px] font-medium ${isActive ? 'text-brand-600' : 'text-slate-500'}`}
          >
            <span aria-hidden className="text-lg leading-none">
              {t.icon}
            </span>
            {t.label}
          </NavLink>
        ))}
      </nav>
    </div>
  )
}
