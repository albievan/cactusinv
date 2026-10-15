import type { ComponentType, SVGProps } from 'react'
import { IconBank, IconBuilding, IconServer } from '../../components/icons'

/** A Settings sub-page: its route, its label in the sidebar and on More, and what it holds. */
export type SettingsItem = {
  /** Route segment under /settings. */
  path: string
  label: string
  hint: string
  icon: ComponentType<SVGProps<SVGSVGElement>>
}

/**
 * The Settings sub-pages, in menu order. The sidebar, the phone More page, the Settings
 * section links and the /settings redirect (to the first item) all read this list, so a
 * new page — "Users" and "Backup & restore" are planned — is one entry here plus its route
 * in App.tsx.
 */
export const settingsNav: SettingsItem[] = [
  { path: 'business', label: 'Business', hint: 'Name, address, VAT, invoice numbers, disclaimer and e-mail text', icon: IconBuilding },
  { path: 'email', label: 'E-mail server', hint: 'The SMTP server invoices and statements are sent through', icon: IconServer },
  { path: 'currencies', label: 'Currencies & bank accounts', hint: 'Currencies and the bank account printed for each', icon: IconBank },
]

/** settingsHref is the absolute route of a Settings sub-page. */
export const settingsHref = (item: SettingsItem) => `/settings/${item.path}`
