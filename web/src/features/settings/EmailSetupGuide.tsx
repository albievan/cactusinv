import { useState, type ReactNode } from 'react'
import { Sheet } from '../../components/ui'

/** The providers the guide explains, in the order the form offers them. */
export const guideProviders = ['Gmail', 'Outlook / Microsoft 365', 'Custom'] as const
export type GuideProvider = (typeof guideProviders)[number]

const Path = ({ children }: { children: ReactNode }) => <span className="font-medium text-slate-800">{children}</span>

// Step by step, what to do at the provider and then on this page. It is help
// text only: the server still tests and stores whatever is entered.
const steps: Record<GuideProvider, ReactNode[]> = {
  Gmail: [
    <>
      Turn on 2-Step Verification for the Google account: <Path>Google Account → Security → 2-Step Verification</Path>. Google
      offers App passwords only to accounts that have it.
    </>,
    <>
      Create an App password: <Path>Google Account → Security → 2-Step Verification → App passwords</Path>. Name it
      &ldquo;cactusInv&rdquo; and copy the 16-letter password Google shows. You see it once.
    </>,
    <>
      Here, choose <Path>Gmail</Path>. The host <Path>smtp.gmail.com</Path>, port <Path>587</Path> and{' '}
      <Path>STARTTLS</Path> are filled in.
    </>,
    <>
      User name: your full Gmail address. Password: the App password, without the spaces &mdash; not your normal password.
    </>,
    <>
      Send as: your Gmail address. To send as another address (for example billing@yourdomain), first add it in Gmail
      under <Path>Settings → Accounts → Send mail as</Path>; otherwise Gmail replaces it with your Gmail address.
    </>,
    <>
      Tick <Path>Use this server for all e-mail</Path>, press <Path>Save e-mail server</Path>, then{' '}
      <Path>Send test e-mail</Path> and check the inbox.
    </>,
  ],
  'Outlook / Microsoft 365': [
    <>
      Make sure the mailbox may send through SMTP. An administrator turns it on in the{' '}
      <Path>Microsoft 365 admin center → Users → Active users → the user → Mail → Manage email apps → Authenticated SMTP</Path>
      . Some tenants switch it off for everyone; ask your administrator if the test is refused.
    </>,
    <>
      Here, choose <Path>Outlook / Microsoft 365</Path>. The host <Path>smtp.office365.com</Path>, port <Path>587</Path>{' '}
      and <Path>STARTTLS</Path> are filled in.
    </>,
    <>User name: the full e-mail address of the mailbox. Password: its password (or an app password, if your organisation uses them).</>,
    <>
      Send as: the mailbox address, or an address the mailbox has been given &ldquo;Send as&rdquo; permission for by an
      administrator.
    </>,
    <>
      Tick <Path>Use this server for all e-mail</Path>, press <Path>Save e-mail server</Path>, then{' '}
      <Path>Send test e-mail</Path> and check the inbox.
    </>,
  ],
  Custom: [
    <>
      Look up your provider&rsquo;s SMTP settings (their help pages call them &ldquo;SMTP&rdquo; or &ldquo;outgoing mail
      server&rdquo;): the host, the port and the security.
    </>,
    <>
      Choose <Path>Custom</Path> and enter them. Port <Path>587</Path> goes with <Path>STARTTLS</Path>, port{' '}
      <Path>465</Path> with <Path>SSL/TLS</Path>. Use <Path>None</Path> only for a test mail server on your own network:
      it sends the password unencrypted.
    </>,
    <>
      User name and password as the provider gives them &mdash; often the full e-mail address and its password; some
      services use a fixed user name and an API key instead.
    </>,
    <>
      Send as: an address on a domain the provider allows you to send from. Set up the SPF and DKIM records the provider
      asks for, so that invoices are not marked as spam.
    </>,
    <>
      Tick <Path>Use this server for all e-mail</Path>, press <Path>Save e-mail server</Path>, then{' '}
      <Path>Send test e-mail</Path> and check the inbox.
    </>,
  ],
}

const trouble: ReactNode[] = [
  <>
    <Path>The password is refused:</Path> for Gmail use an App password, not your normal one; for Microsoft 365 check that
    Authenticated SMTP is on for the mailbox.
  </>,
  <>
    <Path>The test times out:</Path> the network may block outgoing mail ports &mdash; try the other port (587 or 465)
    with its matching security, or ask whoever runs the network.
  </>,
  <>
    <Path>The test e-mail does not arrive:</Path> look in the spam folder, and check the Send as address is one the
    provider lets you use.
  </>,
]

/** EmailSetupGuide explains, step by step, how to set up the e-mail server for each provider. */
export function EmailSetupGuide({ open, onClose, provider }: { open: boolean; onClose: () => void; provider: GuideProvider }) {
  const [shown, setShown] = useState<GuideProvider>(provider)
  const [lastProvider, setLastProvider] = useState(provider)
  // The guide opens on the provider the form has selected.
  if (provider !== lastProvider) {
    setLastProvider(provider)
    setShown(provider)
  }
  return (
    <Sheet title="Set up the e-mail server" open={open} onClose={onClose}>
      <div className="space-y-4 text-sm text-slate-600">
        <div className="flex flex-wrap gap-2" role="tablist" aria-label="Provider">
          {guideProviders.map((name) => (
            <button
              key={name}
              type="button"
              role="tab"
              aria-selected={shown === name}
              className={shown === name ? 'btn-primary' : 'btn-secondary'}
              onClick={() => setShown(name)}
            >
              {name}
            </button>
          ))}
        </div>
        <ol className="list-decimal space-y-2 pl-5" aria-label={`Steps for ${shown}`}>
          {steps[shown].map((step, i) => (
            <li key={i}>{step}</li>
          ))}
        </ol>
        <div className="rounded-xl bg-cream-50 p-3 ring-1 ring-cream-200">
          <h3 className="mb-2 font-semibold text-slate-800">If the test e-mail fails</h3>
          <ul className="list-disc space-y-1 pl-5">
            {trouble.map((item, i) => (
              <li key={i}>{item}</li>
            ))}
          </ul>
        </div>
      </div>
    </Sheet>
  )
}
