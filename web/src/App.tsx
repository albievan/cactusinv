import { lazy, Suspense, type ComponentType } from 'react'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { NoAccess, RequireAccess } from './components/Access'
import { AuthProvider, useCan } from './components/auth'
import { Layout, useNav } from './components/Layout'
import { Dashboard } from './features/dashboard/Dashboard'
import { InvoiceDetail } from './features/invoices/InvoiceDetail'
import { InvoiceEditor } from './features/invoices/InvoiceEditor'
import { InvoiceList } from './features/invoices/InvoiceList'
import { settingsHref, useSettingsNav } from './features/settings/settingsNav'

import { Spinner } from './components/ui'

// Pages other than the dashboard and invoices load when first opened: the first download
// stays small (one bundle was over 500 kB). Each is its own chunk.
function lazyPage<K extends string>(load: () => Promise<Record<K, ComponentType>>, name: K) {
  return lazy(() => load().then((m) => ({ default: m[name] })))
}
const CustomerEditor = lazyPage(() => import('./features/customers/Customers'), 'CustomerEditor')
const CustomerList = lazyPage(() => import('./features/customers/Customers'), 'CustomerList')
const Outbox = lazyPage(() => import('./features/emails/Outbox'), 'Outbox')
const AuditLog = lazyPage(() => import('./features/audit/AuditLog'), 'AuditLog')
const StatementView = lazyPage(() => import('./features/statements/Statements'), 'StatementView')
const Statements = lazyPage(() => import('./features/statements/Statements'), 'Statements')
const InvoicePaidPage = lazyPage(() => import('./features/invoices/InvoicePaidPage'), 'InvoicePaidPage')
const AccountPage = lazyPage(() => import('./features/portal/AccountPage'), 'AccountPage')
const ProductEditor = lazyPage(() => import('./features/products/Products'), 'ProductEditor')
const ProductList = lazyPage(() => import('./features/products/Products'), 'ProductList')
const ScheduleDetailPage = lazyPage(() => import('./features/schedules/Schedules'), 'ScheduleDetailPage')
const ScheduleEditor = lazyPage(() => import('./features/schedules/Schedules'), 'ScheduleEditor')
const ScheduleList = lazyPage(() => import('./features/schedules/Schedules'), 'ScheduleList')
const AccountSettingsPage = lazyPage(() => import('./features/settings/Settings'), 'AccountSettingsPage')
const BusinessSettingsPage = lazyPage(() => import('./features/settings/Settings'), 'BusinessSettingsPage')
const CurrencySettingsPage = lazyPage(() => import('./features/settings/Settings'), 'CurrencySettingsPage')
const EmailSettingsPage = lazyPage(() => import('./features/settings/Settings'), 'EmailSettingsPage')
const More = lazyPage(() => import('./features/settings/Settings'), 'More')
const BackupSettingsPage = lazyPage(() => import('./features/settings/backup/Backup'), 'BackupSettingsPage')
const ImportExportSettingsPage = lazyPage(() => import('./features/settings/transfer/ImportExport'), 'ImportExportSettingsPage')
const SystemSettingsPage = lazyPage(() => import('./features/settings/system/SystemSettings'), 'SystemSettingsPage')
const DatabaseStatusPage = lazyPage(() => import('./features/settings/system/DatabaseStatus'), 'DatabaseStatusPage')
const UsersSettingsPage = lazyPage(() => import('./features/settings/users/Users'), 'UsersSettingsPage')
const QuoteList = lazyPage(() => import('./features/quotes/QuoteList'), 'QuoteList')
const QuoteEditor = lazyPage(() => import('./features/quotes/QuoteEditor'), 'QuoteEditor')
const QuoteDetail = lazyPage(() => import('./features/quotes/QuoteDetail'), 'QuoteDetail')
const QuoteResponsePage = lazyPage(() => import('./features/quotes/QuoteResponsePage'), 'QuoteResponsePage')
const ResetPasswordPage = lazyPage(() => import('./components/ResetPasswordPage'), 'ResetPasswordPage')
const AdhocCustomers = lazyPage(() => import('./features/quotes/AdhocCustomers'), 'AdhocCustomers')
const QuoteSettingsPage = lazyPage(() => import('./features/settings/QuoteSettings'), 'QuoteSettingsPage')

export function AppRoutes() {
  const need = (p: string | string[], el: JSX.Element) => <RequireAccess need={p}>{page(el)}</RequireAccess>
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route index element={<Home />} />
        <Route path="invoices" element={need('invoices.view', <InvoiceList />)} />
        <Route path="invoices/new" element={need('invoices.create', <InvoiceEditor />)} />
        <Route path="invoices/:id" element={need('invoices.view', <InvoiceDetail />)} />
        <Route path="invoices/:id/edit" element={need('invoices.edit', <InvoiceEditor />)} />
        <Route path="quotes" element={need('quotes.view', <QuoteList />)} />
        <Route path="quotes/new" element={need('quotes.create', <QuoteEditor />)} />
        <Route path="quotes/adhoc-customers" element={need('quotes.view', <AdhocCustomers />)} />
        <Route path="quotes/:id" element={need('quotes.view', <QuoteDetail />)} />
        <Route path="quotes/:id/edit" element={need('quotes.edit', <QuoteEditor />)} />
        <Route path="customers" element={need('customers.view', <CustomerList />)} />
        <Route path="customers/new" element={need('customers.create', <CustomerEditor />)} />
        <Route path="customers/:id" element={need('customers.view', <CustomerEditor />)} />
        <Route path="products" element={need('products.view', <ProductList />)} />
        <Route path="products/new" element={need('products.create', <ProductEditor />)} />
        <Route path="products/:id" element={need('products.view', <ProductEditor />)} />
        <Route path="schedules" element={need('schedules.view', <ScheduleList />)} />
        <Route path="schedules/new" element={need('schedules.create', <ScheduleEditor />)} />
        <Route path="schedules/:id" element={need('schedules.view', <ScheduleDetailPage />)} />
        <Route path="schedules/:id/edit" element={need('schedules.edit', <ScheduleEditor />)} />
        <Route path="emails" element={need('emails.view', <Outbox />)} />
        <Route path="statements" element={need('statements.view', <Statements />)} />
        <Route path="statements/:customerId" element={need('statements.view', <StatementView />)} />
        <Route path="audit" element={need('audit.view', <AuditLog />)} />
        <Route path="more" element={page(<More />)} />
        <Route path="settings">
          <Route index element={<SettingsHome />} />
          <Route path="business" element={need('settings.business.view', <BusinessSettingsPage />)} />
          <Route path="quotes" element={need('settings.quotes.view', <QuoteSettingsPage />)} />
          <Route path="email" element={need('settings.email.view', <EmailSettingsPage />)} />
          <Route path="currencies" element={need('settings.currencies.view', <CurrencySettingsPage />)} />
          <Route path="import-export" element={need(['settings.export', 'settings.import'], <ImportExportSettingsPage />)} />
          <Route path="system" element={need('settings.system.view', <SystemSettingsPage />)} />
          <Route path="database" element={need('settings.database.view', <DatabaseStatusPage />)} />
          <Route path="users" element={need('administer', <UsersSettingsPage />)} />
          <Route path="backup" element={need('administer', <BackupSettingsPage />)} />
          <Route path="account" element={page(<AccountSettingsPage />)} />
          <Route path="*" element={<SettingsHome />} />
        </Route>
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}

/** page shows a spinner while a page's code loads. */
function page(el: JSX.Element) {
  return <Suspense fallback={<Spinner />}>{el}</Suspense>
}

/** Home is the dashboard, or the first page the user's groups allow (ADR-012). */
function Home() {
  const can = useCan()
  const first = useNav().find((t) => t.to !== '/settings')
  if (can('dashboard.view')) return <Dashboard />
  if (first) return <Navigate to={first.to} replace />
  // Settings is always there (Your account, perhaps more): better than an empty start page.
  return <Navigate to="/settings" replace />
}

/** SettingsHome opens the first Settings page the user may see. */
function SettingsHome() {
  const first = useSettingsNav()[0]
  return first ? <Navigate to={settingsHref(first)} replace /> : <NoAccess />
}

export function App() {
  return (
    <BrowserRouter>
      <Routes>
        {/* The customer's answer to a quote e-mail (M41): no sign-in. */}
        <Route path="/q/:token" element={page(<QuoteResponsePage />)} />
        {/* The customer's "I have paid" page for an invoice e-mail (M42): no sign-in. */}
        <Route path="/i/:token" element={page(<InvoicePaidPage />)} />
        {/* The customer's account page for a statement e-mail (M59): no sign-in. */}
        <Route path="/a/:token" element={page(<AccountPage />)} />
        {/* A forgotten password's e-mailed link (M43): no sign-in. */}
        <Route path="/reset-password/:token" element={page(<ResetPasswordPage />)} />
        <Route path="*" element={<AuthProvider><AppRoutes /></AuthProvider>} />
      </Routes>
    </BrowserRouter>
  )
}
