import type { SVGProps } from 'react'

// A small, dependency-free stroke icon set.
const base = (d: React.ReactNode) =>
  function Icon(props: SVGProps<SVGSVGElement>) {
    return (
      <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" {...props}>
        {d}
      </svg>
    )
  }

export const IconHome = base(<><path d="M3 11l9-7 9 7" /><path d="M5 10v10h14V10" /></>)
export const IconInvoice = base(<><path d="M6 3h9l4 4v14H6z" /><path d="M14 3v5h5M9 13h6M9 17h6" /></>)
export const IconCalendar = base(<><rect x="3" y="5" width="18" height="16" rx="2" /><path d="M3 10h18M8 3v4M16 3v4" /></>)
export const IconUsers = base(<><circle cx="9" cy="8" r="3.5" /><path d="M2.5 20c.8-3.5 3.4-5.5 6.5-5.5s5.7 2 6.5 5.5" /><path d="M16 4.5a3.5 3.5 0 010 7M18 14.6c1.8.8 3 2.6 3.5 5.4" /></>)
export const IconBox = base(<><path d="M3 7l9-4 9 4-9 4z" /><path d="M3 7v10l9 4 9-4V7M12 11v10" /></>)
export const IconMail = base(<><rect x="3" y="5" width="18" height="14" rx="2" /><path d="M3 7l9 6 9-6" /></>)
export const IconSettings = base(<><circle cx="12" cy="12" r="3" /><path d="M19.4 15a1.7 1.7 0 00.3 1.8l.1.1a2 2 0 11-2.8 2.8l-.1-.1a1.7 1.7 0 00-1.8-.3 1.7 1.7 0 00-1 1.5V21a2 2 0 11-4 0v-.1a1.7 1.7 0 00-1.1-1.5 1.7 1.7 0 00-1.8.3l-.1.1a2 2 0 11-2.8-2.8l.1-.1a1.7 1.7 0 00.3-1.8 1.7 1.7 0 00-1.5-1H3a2 2 0 110-4h.1a1.7 1.7 0 001.5-1.1 1.7 1.7 0 00-.3-1.8l-.1-.1a2 2 0 112.8-2.8l.1.1a1.7 1.7 0 001.8.3H9a1.7 1.7 0 001-1.5V3a2 2 0 114 0v.1a1.7 1.7 0 001 1.5 1.7 1.7 0 001.8-.3l.1-.1a2 2 0 112.8 2.8l-.1.1a1.7 1.7 0 00-.3 1.8V9a1.7 1.7 0 001.5 1H21a2 2 0 110 4h-.1a1.7 1.7 0 00-1.5 1z" /></>)
export const IconSearch = base(<><circle cx="11" cy="11" r="7" /><path d="M20 20l-3.5-3.5" /></>)
export const IconUser = base(<><circle cx="12" cy="8" r="4" /><path d="M4 21c1-4 4-6 8-6s7 2 8 6" /></>)
export const IconLogout = base(<><path d="M10 17l-5-5 5-5M5 12h11" /><path d="M14 4h4a2 2 0 012 2v12a2 2 0 01-2 2h-4" /></>)
export const IconFold = base(<><path d="M11 17l-5-5 5-5M18 17l-5-5 5-5" /></>)
export const IconUnfold = base(<><path d="M13 7l5 5-5 5M6 7l5 5-5 5" /></>)
export const IconMenu = base(<><path d="M4 7h16M4 12h16M4 17h16" /></>)
export const IconPlus = base(<><path d="M12 5v14M5 12h14" /></>)
export const IconDownload = base(<><path d="M12 4v11M7 10l5 5 5-5M5 20h14" /></>)
export const IconSend = base(<><path d="M21 3L10 14M21 3l-7 18-4-7-7-4z" /></>)
export const IconCheck = base(<path d="M5 12l5 5 9-10" />)
export const IconStatement = base(<><path d="M5 3h14v18H5z" /><path d="M8 8h8M8 12h8M8 16h5" /></>)
export const IconFilePdf = base(<><path d="M6 3h9l4 4v14H6z" /><path d="M14 3v5h5" /><path d="M9 14h1.5a1.5 1.5 0 000-3H9v6M14 11v6M14 11h2M14 14h1.5" /></>)
export const IconPaid = base(<><circle cx="12" cy="12" r="9" /><path d="M8 12.5l2.5 2.5L16 9.5" /></>)
export const IconClockAlert = base(<><circle cx="12" cy="12" r="9" /><path d="M12 7v5l3 2" /></>)
export const IconBan = base(<><circle cx="12" cy="12" r="9" /><path d="M5.6 5.6l12.8 12.8" /></>)
export const IconShield = base(<><path d="M12 3l8 3v6c0 4.5-3.4 8.4-8 9-4.6-.6-8-4.5-8-9V6z" /><path d="M9 12l2 2 4-4" /></>)
export const IconHistory = base(<><path d="M3 12a9 9 0 103-6.7L3 8" /><path d="M3 3v5h5M12 7v5l3 2" /></>)
