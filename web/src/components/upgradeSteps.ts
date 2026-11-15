// Step-by-step upgrades to a newer version, per way cactusInv was installed and the operating
// system of the computer they are run from: the Upgrade tab of InstallCommands, on the sign-in
// page and in System Settings → Version. A standalone program upgrades itself from System
// Settings (or with its setup script again); a container install with deploy.sh --upgrade.

import { containerPlatforms, type ContainerPlatform, type HostOS, type Step } from './containerSteps'

const RAW = 'https://raw.githubusercontent.com/albievan/cactusinv/main'

export type UpgradeMethod = 'standalone' | ContainerPlatform

export const upgradeMethods: { key: UpgradeMethod; label: string; os: HostOS[] }[] = [
  { key: 'standalone', label: 'Standalone program', os: ['linux', 'windows', 'macos'] },
  ...containerPlatforms,
]

const flags: Record<ContainerPlatform, string> = { docker: 'dockerdesktop', podman: 'podman', k3s: 'k3s', k8s: 'k8s' }

const check: Step = {
  title: 'See whether a newer version is out',
  text: 'Sign in as an administrator and open Settings → System Settings → Version, then press Check for updates. It shows the version running now and the newest one published.',
}

function standalone(os: HostOS): Step[] {
  const windows = os === 'windows'
  return [
    check,
    {
      title: 'Easiest: install it from cactusInv',
      text: 'On the same page press Install and restart, then confirm. cactusInv takes a backup first (when a backup key is set up), downloads the new program for this computer, checks its signature, keeps the old one as cactusinv.old and restarts. Nobody is signed out; the database is updated as it starts. If the new version does not start, the old one is put back.',
    },
    {
      title: 'Or by hand: run the setup script again',
      text: `${windows ? 'In PowerShell, in' : 'In a terminal, in'} the folder cactusinv lives in. It downloads the newest release, checks it and keeps the old program as ${windows ? 'cactusinv.old.exe' : 'cactusinv.old'}. Your data and settings stay where they are.`,
      commands: windows
        ? [
            { label: 'The newest version', command: `irm ${RAW}/setup.ps1 | iex` },
            { label: 'A particular version (put its number in place of 1.31.0)', command: `$env:CACTUSINV_VERSION = "1.31.0"; irm ${RAW}/setup.ps1 | iex` },
          ]
        : [
            { label: 'The newest version', command: `curl -fsSL ${RAW}/setup.sh | sh` },
            { label: 'A particular version (put its number in place of 1.31.0)', command: `curl -fsSL ${RAW}/setup.sh | CACTUSINV_VERSION=1.31.0 sh` },
          ],
    },
    {
      title: 'Restart cactusinv',
      text: 'Stop it and start it again the way you normally do (in its terminal with Ctrl+C, then the same start command; or your service manager). The new version updates the database as it starts.',
      commands: [{ label: 'Which version is here', command: windows ? '.\\cactusinv.exe version' : './cactusinv version' }],
    },
    {
      title: 'Check',
      text: 'Sign in: Settings → System Settings → Version shows the new version. If something went wrong, the backup taken before the upgrade is listed under Settings → Backup.',
    },
  ]
}

function container(platform: ContainerPlatform, os: HostOS): Step[] {
  const flag = flags[platform]
  const windows = os === 'windows'
  const remote = platform === 'k3s'
  const steps: Step[] = [check]
  if (windows) {
    steps.push({
      title: 'Open Ubuntu',
      text: `deploy.sh runs in Ubuntu on WSL 2: open "Ubuntu" from the Start menu.${platform === 'docker' || platform === 'k8s' ? ' Docker Desktop must be running.' : ''}`,
    })
  }
  steps.push({
    title: 'Go to the folder you installed from',
    text: 'The cactusinv folder (git checkout) you ran deploy.sh --install in. Leave its files as they are: an upgrade stops if they were changed. Your own settings belong in .env.local, which upgrades leave alone.',
    commands: [{ command: 'cd ~/cactusinv' }],
  })
  if (platform === 'k8s') {
    steps.push({
      title: 'Check the cluster and registry',
      text: 'The new image is built here and pushed to K8S_REGISTRY, as at the install: kubectl must reach the cluster and docker must be signed in to the registry.',
      commands: [{ command: 'kubectl get nodes' }],
    })
  }
  const upgradeText = 'It fetches the newest version, shows the version running and the new one, the changes in between and whether the database changes, and asks you to type upgrade. It takes a backup first, installs the new version and checks that it runs; if it does not, it goes back to the old version with the data as it was. cactusInv is unavailable for a minute or two; nobody is signed out.'
  steps.push({
    title: 'Upgrade',
    text: remote ? `${upgradeText} Run it on the server, or from your own computer over SSH.` : upgradeText,
    commands: remote
      ? [
          ...(windows ? [] : [{ label: 'On the server (as root)', command: `./deploy.sh --upgrade --${flag}` }]),
          { label: 'From your computer over SSH', command: `./deploy.sh --upgrade --${flag} --ssh-access root@your-server` },
          { label: 'A particular version (put its number in place of 1.31.0)', command: `./deploy.sh --upgrade --to v1.31.0 --${flag}${windows ? ' --ssh-access root@your-server' : ''}` },
        ]
      : [
          { label: 'The newest version', command: `./deploy.sh --upgrade --${flag}` },
          { label: 'A particular version (put its number in place of 1.31.0)', command: `./deploy.sh --upgrade --to v1.31.0 --${flag}` },
        ],
  })
  const ssh = remote && windows ? ' --ssh-access root@your-server' : ''
  steps.push({
    title: 'Check',
    text: 'Shows the version running and its database schema. Signed in, Settings → System Settings → Version shows it too.',
    commands: [{ command: `./deploy.sh --version --${flag}${ssh}` }],
  })
  return steps
}

/** upgradeSteps are the steps to upgrade an installation of method from an operating system. */
export function upgradeSteps(method: UpgradeMethod, os: HostOS): Step[] {
  return method === 'standalone' ? standalone(os) : container(method, os)
}
