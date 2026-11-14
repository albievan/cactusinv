import { useState } from 'react'

/** The password policy (domain.ValidatePassword): length in characters, not bytes. */
export const MIN_PASSWORD = 12
export const MAX_PASSWORD = 200

const chars = (s: string) => [...s].length

/**
 * PasswordInput is a password box with an eye button at its end that shows or hides what
 * is typed. Pass show/onShow to share one eye between boxes; without them it keeps its own.
 */
export function PasswordInput({ id, autoComplete, placeholder, value, onChange, show, onShow, required = true, describedBy }: {
  id: string
  autoComplete: string
  placeholder?: string
  value: string
  onChange: (v: string) => void
  show?: boolean
  onShow?: (v: boolean) => void
  required?: boolean
  describedBy?: string
}) {
  const [own, setOwn] = useState(false)
  const visible = show ?? own
  const toggle = () => (onShow ? onShow(!visible) : setOwn(!visible))
  return (
    <div className="input-group">
      <input id={id} className="form-control" type={visible ? 'text' : 'password'} autoComplete={autoComplete} placeholder={placeholder}
        maxLength={autoComplete === 'new-password' ? MAX_PASSWORD : undefined} aria-describedby={describedBy}
        value={value} onChange={(e) => onChange(e.target.value)} required={required} />
      <button type="button" className="btn password-toggle" aria-label={visible ? 'Hide password' : 'Show password'} aria-pressed={visible} title={visible ? 'Hide password' : 'Show password'} aria-controls={id} onClick={toggle}>
        <i className={`bi ${visible ? 'bi-eye-slash' : 'bi-eye'}`} style={{ fontSize: '18px' }} aria-hidden="true" />
      </button>
    </div>
  )
}

/** passwordProblem is why a new password (and its confirmation) cannot be used yet; "" when it can. */
export function passwordProblem(next: string, confirm: string, current?: string): string {
  const n = chars(next)
  if (n < MIN_PASSWORD) return `The new password needs at least ${MIN_PASSWORD} characters (it has ${n}).`
  if (n > MAX_PASSWORD) return `The new password may have at most ${MAX_PASSWORD} characters.`
  if (current !== undefined && current !== '' && next === current) return 'The new password must be different from the current one.'
  if (next !== confirm) return 'The new password and its confirmation are not the same.'
  return ''
}

/**
 * PasswordPolicy states the password rules and ticks them off as the new password and its
 * confirmation are typed.
 */
export function PasswordPolicy({ id, next, confirm, current }: { id?: string; next: string; confirm: string; current?: string }) {
  const n = chars(next)
  const rules: [string, boolean][] = [
    [`${MIN_PASSWORD} to ${MAX_PASSWORD} characters (now ${n})`, n >= MIN_PASSWORD && n <= MAX_PASSWORD],
    ['New password and confirmation are the same', next !== '' && next === confirm],
  ]
  if (current !== undefined) rules.push(['Different from the current password', next !== '' && next !== current])
  return (
    <div id={id} className="rounded-3 border bg-body-tertiary p-3 small">
      <div className="fw-semibold mb-1">Password policy</div>
      <ul className="list-unstyled mb-2" aria-label="Password rules">
        {rules.map(([label, ok]) => (
          <li key={label} className={ok ? 'text-success-emphasis' : 'text-body-secondary'}>
            <i className={`bi ${ok ? 'bi-check-circle-fill' : 'bi-circle'} me-2`} aria-hidden="true" />
            {label}
            <span className="visually-hidden">{ok ? ' (met)' : ' (not met yet)'}</span>
          </li>
        ))}
      </ul>
      <div className="text-body-secondary">
        Any characters may be used, spaces included; upper and lower case are different. A few unrelated words make a strong password that is easy to remember.
        Changing it signs you out everywhere else.
      </div>
    </div>
  )
}
