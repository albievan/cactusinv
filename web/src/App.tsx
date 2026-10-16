import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { NoAccess, RequireAccess } from './components/Access'
import { AuthProvider, useCan } from './components/auth'
import { Layout, useNav } from './components/Layout'
import { CustomerEditor, CustomerList } from './features/customers/Customers'
import { Dashboard } from './features/dashboard/Dashboard'
import { Outbox } from './features/emails/Outbox'
import { AuditLog } from './features/audit/AuditLog'
import { StatementView, Statements } from './features/statements/Statements'
import { InvoiceDetail } from './features/invoices/InvoiceDetail'
import { InvoiceEditor } from './features/invoices/InvoiceEditor'
import { InvoiceList } from './features/invoices/InvoiceList'
import { ProductEditor, ProductList } from './features/products/Products'
import { ScheduleDetailPage, ScheduleEditor, ScheduleList } from './features/schedules/Schedules'
import { AccountSettingsPage, BusinessSettingsPage, CurrencySettingsPage, EmailSettingsPage, More } from './features/settings/Settings'
import { BackupSettingsPage } from './features/settings/backup/Backup'
import { UsersSettingsPage } from './features/settings/users/Users'
import { settingsHref, useSettingsNav } from './features/settings/settingsNav'

export function AppRoutes() {
  const need = (p: string, el: JSX.Element) => <RequireAccess need={p}>{el}</RequireAccess>
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route index element={<Home />} />
        <Route path="invoices" element={need('invoices.view', <InvoiceList />)} />
        <Route path="invoices/new" element={need('invoices.edit', <InvoiceEditor />)} />
        <Route path="invoices/:id" element={need('invoices.view', <InvoiceDetail />)} />
        <Route path="invoices/:id/edit" element={need('invoices.edit', <InvoiceEditor />)} />
        <Route path="customers" element={need('customers.view', <CustomerList />)} />
        <Route path="customers/new" element={need('customers.edit', <CustomerEditor />)} />
        <Route path="customers/:id" element={need('customers.view', <CustomerEditor />)} />
        <Route path="products" element={need('products.view', <ProductList />)} />
        <Route path="products/new" element={need('products.edit', <ProductEditor />)} />
        <Route path="products/:id" element={need('products.view', <ProductEditor />)} />
        <Route path="schedules" element={need('schedules.view', <ScheduleList />)} />
        <Route path="schedules/new" element={need('schedules.edit', <ScheduleEditor />)} />
        <Route path="schedules/:id" element={need('schedules.view', <ScheduleDetailPage />)} />
        <Route path="schedules/:id/edit" element={need('schedules.edit', <ScheduleEditor />)} />
        <Route path="emails" element={need('emails.view', <Outbox />)} />
        <Route path="statements" element={need('statements.view', <Statements />)} />
        <Route path="statements/:customerId" element={need('statements.view', <StatementView />)} />
        <Route path="audit" element={need('audit.view', <AuditLog />)} />
        <Route path="more" element={<More />} />
        <Route path="settings">
          <Route index element={<SettingsHome />} />
          <Route path="business" element={need('settings.view', <BusinessSettingsPage />)} />
          <Route path="email" element={need('settings.view', <EmailSettingsPage />)} />
          <Route path="currencies" element={need('settings.view', <CurrencySettingsPage />)} />
          <Route path="users" element={need('administer', <UsersSettingsPage />)} />
          <Route path="backup" element={need('administer', <BackupSettingsPage />)} />
          <Route path="account" element={<AccountSettingsPage />} />
          <Route path="*" element={<SettingsHome />} />
        </Route>
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
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
      <AuthProvider>
        <AppRoutes />
      </AuthProvider>
    </BrowserRouter>
  )
}
