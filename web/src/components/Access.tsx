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
    <div className="mx-auto px-3 py-5 text-center" style={{ maxWidth: '36rem' }}>
      <IconShield width={40} className="text-primary" />
      <h1 className="h4 mt-3">You do not have access to this</h1>
      <p className="mt-2 small text-body-secondary">What you can do comes from the groups you are in. Ask an administrator to put you in a group that allows it.</p>
      {home && (
        <Link to="/" className="btn btn-outline-primary mt-4">
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
    <span className={className?.replace(/(^|\s)(link-primary|link-underline|text-decoration-underline)(?=\s|$)/g, '$1')}>{children}</span>
  )
}
