/** The cactus mark used in the sidebar and on the login screen. */
export function CactusMark({ className, size = 32, color = 'currentColor' }: { className?: string; size?: number; color?: string }) {
  return (
    <svg viewBox="0 0 48 48" width={size} height={size} className={className} aria-hidden="true">
      <path
        fill={color}
        d="M21 6a4 4 0 018 0v30h-8zM10 18a3 3 0 016 0v6a2 2 0 002 2h3v5h-5a6 6 0 01-6-6zM32 13a3 3 0 016 0v9a6 6 0 01-6 6h-3v-5h3a1 1 0 001-1zM17 38h14v3a1 1 0 01-1 1H18a1 1 0 01-1-1z"
      />
    </svg>
  )
}

export function Wordmark({ dark = false, tagline = true }: { dark?: boolean; tagline?: boolean }) {
  return (
    <div className="d-flex align-items-center gap-2">
      <CactusMark size={40} color={dark ? '#e9dfc9' : '#2f5d46'} />
      <div className="lh-1">
        <div className={`fs-3 fw-bold ${dark ? 'text-cream' : 'text-primary-emphasis'}`}>
          cactus<span className={dark ? 'text-gold' : 'text-primary'}>Inv</span>
        </div>
        {tagline && <div className={`mt-1 fw-semibold text-uppercase wordmark-tagline ${dark ? 'text-cream-muted' : 'text-primary-emphasis'}`}>Simple invoicing</div>}
      </div>
    </div>
  )
}
