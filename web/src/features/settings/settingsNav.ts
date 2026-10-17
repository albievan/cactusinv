import type { ComponentType, SVGProps } from 'react'
import { useCan } from '../../components/auth'
import { IconArchive, IconBank, IconBuilding, IconServer, IconSettings, IconShield, IconTransfer, IconUser } from '../../components/icons'

/** A Settings sub-page: its route, its label in the sidebar and on More, and what it holds. */
export type SettingsItem = {
  /** Route segment under /settings. */
  path: string
  label: string
  hint: string
  icon: ComponentType<SVGProps<SVGSVGElement>>
  /** The permission that shows it, or any one of several (ADR-012); none = every signed-in user. */
  need?: string | string[]
}

/**
 * The Settings sub-pages, in menu order. The sidebar, the phone More page, the Settings
 * section links and the /settings redirect (to the first item) all read this list, so a
 * new page is one entry here plus its route in App.tsx.
 */
export const settingsNav: SettingsItem[] = [
  { path: 'business', label: 'Business', hint: 'Name, address, VAT, invoice numbers, disclaimer and e-mail text', icon: IconBuilding, need: 'settings.business.view' },
  { path: 'email', label: 'E-mail server', hint: 'The SMTP server invoices and statements are sent through', icon: IconServer, need: 'settings.email.view' },
  { path: 'currencies', label: 'Currencies & bank accounts', hint: 'Currencies and the bank account printed for each', icon: IconBank, need: 'settings.currencies.view' },
  { path: 'import-export', label: 'Import/Export Settings', hint: 'Customers, products, schedules and the settings as one file', icon: IconTransfer, need: ['settings.export', 'settings.import'] },
  { path: 'system', label: 'System Settings', hint: 'Session timeouts and the folders for backups and the invoice export', icon: IconSettings, need: 'settings.system.view' },
  { path: 'users', label: 'Users & groups', hint: 'Who can sign in and what each group may do', icon: IconUser, need: 'administer' },
  { path: 'backup', label: 'Backup & restore', hint: 'Encrypted backups, downloads, and restoring from a file', icon: IconArchive, need: 'administer' },
  { path: 'account', label: 'Your account', hint: 'Your password', icon: IconShield },
]

/** useSettingsNav is settingsNav without the pages the signed-in user may not open. */
export function useSettingsNav(): SettingsItem[] {
  const can = useCan()
  return settingsNav.filter((s) => !s.need || can(s.need))
}

/** settingsHref is the absolute route of a Settings sub-page. */
export const settingsHref = (item: SettingsItem) => `/settings/${item.path}`
