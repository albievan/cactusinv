import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { AuthProvider } from './components/auth'
import { Layout } from './components/Layout'
import { CustomerEditor, CustomerList } from './features/customers/Customers'
import { Dashboard } from './features/dashboard/Dashboard'
import { Outbox } from './features/emails/Outbox'
import { InvoiceDetail } from './features/invoices/InvoiceDetail'
import { InvoiceEditor } from './features/invoices/InvoiceEditor'
import { InvoiceList } from './features/invoices/InvoiceList'
import { ProductEditor, ProductList } from './features/products/Products'
import { ScheduleDetailPage, ScheduleEditor, ScheduleList } from './features/schedules/Schedules'
import { More, Settings } from './features/settings/Settings'

export function AppRoutes() {
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route index element={<Dashboard />} />
        <Route path="invoices" element={<InvoiceList />} />
        <Route path="invoices/new" element={<InvoiceEditor />} />
        <Route path="invoices/:id" element={<InvoiceDetail />} />
        <Route path="invoices/:id/edit" element={<InvoiceEditor />} />
        <Route path="customers" element={<CustomerList />} />
        <Route path="customers/new" element={<CustomerEditor />} />
        <Route path="customers/:id" element={<CustomerEditor />} />
        <Route path="products" element={<ProductList />} />
        <Route path="products/new" element={<ProductEditor />} />
        <Route path="products/:id" element={<ProductEditor />} />
        <Route path="schedules" element={<ScheduleList />} />
        <Route path="schedules/new" element={<ScheduleEditor />} />
        <Route path="schedules/:id" element={<ScheduleDetailPage />} />
        <Route path="schedules/:id/edit" element={<ScheduleEditor />} />
        <Route path="emails" element={<Outbox />} />
        <Route path="more" element={<More />} />
        <Route path="settings" element={<Settings />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
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
