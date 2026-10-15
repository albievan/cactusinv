/** The cactus mark used in the sidebar and on the login screen. */
export function CactusMark({ className = 'h-8 w-8', color = 'currentColor' }: { className?: string; color?: string }) {
  return (
    <svg viewBox="0 0 48 48" className={className} aria-hidden="true">
      <path
        fill={color}
        d="M21 6a4 4 0 018 0v30h-8zM10 18a3 3 0 016 0v6a2 2 0 002 2h3v5h-5a6 6 0 01-6-6zM32 13a3 3 0 016 0v9a6 6 0 01-6 6h-3v-5h3a1 1 0 001-1zM17 38h14v3a1 1 0 01-1 1H18a1 1 0 01-1-1z"
      />
    </svg>
  )
}

export function Wordmark({ dark = false, tagline = true }: { dark?: boolean; tagline?: boolean }) {
  return (
    <div className="flex items-center gap-2.5">
      <CactusMark className="h-10 w-10" color={dark ? '#e9dfc9' : '#2f5d46'} />
      <div className="leading-none">
        <div className={`text-2xl font-bold tracking-tight ${dark ? 'text-cream-100' : 'text-brand-800'}`}>
          cactus<span className={dark ? 'text-[#d9c89f]' : 'text-brand-500'}>Inv</span>
        </div>
        {tagline && <div className={`mt-1 text-[10px] font-semibold uppercase tracking-[0.25em] ${dark ? 'text-cream-300' : 'text-brand-700'}`}>Simple invoicing</div>}
      </div>
    </div>
  )
}
