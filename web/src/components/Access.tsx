import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { useCan } from './auth'
import { IconShield } from './icons'

/** RequireAccess shows its page only to a user whose groups allow `need`. */
export function RequireAccess({ need, children }: { need: string | string[]; children: ReactNode }) {
  const can = useCan()
  return can(need) ? <>{children}</> : <NoAccess />
}

/** NoAccess is shown instead of a page the signed-in user's groups do not allow. */
export function NoAccess({ home = true }: { home?: boolean }) {
  return (
    <div className="mx-auto max-w-xl px-4 py-16 text-center">
      <IconShield className="mx-auto h-10 w-10 text-brand-700" aria-hidden="true" />
      <h1 className="mt-4 text-xl font-bold text-brand-900">You do not have access to this</h1>
      <p className="mt-2 text-sm text-slate-600">What you can do comes from the groups you are in. Ask an administrator to put you in a group that allows it.</p>
      {home && (
        <Link to="/" className="btn-secondary mt-6 inline-flex">
          Go to the start page
        </Link>
      )}
    </div>
  )
}

/** Can renders its children only for a user whose groups allow `need`. */
export function Can({ need, children }: { need: string | string[]; children: ReactNode }) {
  const can = useCan()
  return can(need) ? <>{children}</> : null
}

/** MaybeLink is a link for a user whose groups allow `need`, and plain text otherwise. */
export function MaybeLink({ need, to, className, children }: { need: string; to: string; className?: string; children: ReactNode }) {
  const can = useCan()
  return can(need) ? (
    <Link to={to} className={className}>
      {children}
    </Link>
  ) : (
    <span className={className?.replace(/\b(hover:underline|underline|text-brand-700)\b/g, '')}>{children}</span>
  )
}
