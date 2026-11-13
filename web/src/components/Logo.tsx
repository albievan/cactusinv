/**
 * The cactus mark: two interlocking line strokes, like circuit traces, each arm ending in a
 * node ring — the left one deep sage, the right one mint. Used in the sidebar, the phone top
 * bar, the sign-in screens and (as the same drawing) favicon.svg.
 */
export const LOGO_DARK = '#68927a'
export const LOGO_LIGHT = '#9ec6b1'

export function CactusMark({ className, size = 32 }: { className?: string; size?: number }) {
  return (
    <svg viewBox="-1 11 56 56" width={size} height={size} className={className} aria-hidden="true" fill="none" strokeWidth={2.4} strokeLinecap="round" strokeLinejoin="round">
      <g stroke={LOGO_LIGHT}>
        <path d="M22 38V19.25a4.75 4.75 0 0 1 9.5 0V47l6.6-3.1Q40 43 40 41v-8.7" />
        <circle cx="40" cy="30.5" r="1.8" />
      </g>
      <g stroke={LOGO_DARK}>
        <path d="M13.4 26.4V35q0 3 3.1 4.5l3.5 1.7q2.5 1.2 2.5 3.8V60a4.5 4.5 0 0 0 9 0v-6" />
        <circle cx="13.4" cy="24.6" r="1.8" />
      </g>
    </svg>
  )
}

export function Wordmark({ dark = false, tagline = true }: { dark?: boolean; tagline?: boolean }) {
  return (
    <div className="d-flex align-items-center gap-2">
      <CactusMark size={40} />
      <div className="lh-1">
        <div className={`fs-3 fw-bold ${dark ? 'text-cream' : 'text-primary-emphasis'}`}>
          cactus<span className={dark ? 'text-gold' : 'text-primary'}>Inv</span>
        </div>
        {tagline && <div className={`mt-1 fw-semibold text-uppercase wordmark-tagline ${dark ? 'text-cream-muted' : 'text-primary-emphasis'}`}>Simple quotes and invoicing</div>}
      </div>
    </div>
  )
}
