// Bootstrap Icons 1.13.2 (the icon font, loaded in main.tsx). Each icon is decorative:
// the button or link around it carries the name (text, or aria-label and title).

export type IconProps = { width?: number; height?: number; className?: string; 'aria-hidden'?: boolean | 'true' | 'false' }

/** bi makes an icon component for one Bootstrap Icons name; `width` sets its size in px. */
function bi(name: string) {
  return function Icon({ width = 20, className }: IconProps) {
    return <i className={`bi bi-${name}${className ? ` ${className}` : ''}`} style={{ fontSize: `${width}px` }} aria-hidden="true" />
  }
}

export type Icon = ReturnType<typeof bi>

export const IconHome = bi('house')
export const IconInvoice = bi('receipt')
export const IconCalendar = bi('calendar3')
export const IconUsers = bi('people')
export const IconBox = bi('box-seam')
export const IconMail = bi('envelope')
export const IconSettings = bi('gear')
export const IconSearch = bi('search')
export const IconUser = bi('person-circle')
export const IconLogout = bi('box-arrow-right')
export const IconFold = bi('chevron-double-left')
export const IconUnfold = bi('chevron-double-right')
export const IconMenu = bi('list')
export const IconPlus = bi('plus-lg')
export const IconDownload = bi('download')
export const IconSend = bi('send')
export const IconCheck = bi('check-lg')
export const IconRenew = bi('arrow-repeat')
export const IconStatement = bi('journal-text')
export const IconFilePdf = bi('file-earmark-pdf')
export const IconPaid = bi('check-circle')
export const IconClockAlert = bi('hourglass-split')
export const IconBan = bi('slash-circle')
export const IconShield = bi('shield-check')
export const IconInfo = bi('info-circle')
export const IconHistory = bi('clock-history')
export const IconBuilding = bi('building')
export const IconBank = bi('bank')
export const IconServer = bi('hdd-stack')
export const IconArchive = bi('archive')
export const IconEdit = bi('pencil')
export const IconPause = bi('pause-fill')
export const IconPlay = bi('play-fill')
export const IconBolt = bi('lightning-charge')
export const IconTransfer = bi('arrow-left-right')
export const IconTrash = bi('trash')
export const IconClose = bi('x-lg')
export const IconBack = bi('arrow-left')
export const IconQuote = bi('file-earmark-text')
export const IconPersonPlus = bi('person-plus')
