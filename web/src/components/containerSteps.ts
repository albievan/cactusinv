// Step-by-step container installs with deploy.sh, per container platform and the operating
// system of the computer they are run from: shown on the sign-in page and in System Settings →
// Version (the Container tab of InstallCommands). deploy.sh is a bash script, so on Windows the
// steps run in Ubuntu on WSL 2.

export type ContainerPlatform = 'docker' | 'podman' | 'k3s' | 'k8s'
export type HostOS = 'linux' | 'windows' | 'macos'

export type StepCommand = { label?: string; command: string }
export type Step = { title: string; text?: string; commands?: StepCommand[] }

export const containerPlatforms: { key: ContainerPlatform; label: string; os: HostOS[] }[] = [
  { key: 'docker', label: 'Docker', os: ['linux', 'windows', 'macos'] },
  { key: 'podman', label: 'Podman', os: ['linux', 'windows'] },
  { key: 'k3s', label: 'k3s', os: ['linux', 'windows'] },
  { key: 'k8s', label: 'Kubernetes', os: ['linux', 'windows'] },
]

export const hostOSLabels: Record<HostOS, string> = { linux: 'Linux', windows: 'Windows', macos: 'macOS' }

/** containerPlatformOf is the Container tab's platform for a deploy.sh target ("" when none). */
export function containerPlatformOf(target: string): ContainerPlatform | '' {
  return ({ dockerdesktop: 'docker', podman: 'podman', k3s: 'k3s', k8s: 'k8s' } as Record<string, ContainerPlatform>)[target] ?? ''
}

const REPO = 'https://github.com/albievan/cactusinv.git'

const wsl: Step = {
  title: 'Install Ubuntu on WSL 2',
  text: 'deploy.sh is a bash script, so it runs in Ubuntu on WSL 2. In PowerShell opened as administrator, then restart Windows if asked. Open "Ubuntu" from the Start menu and choose a user name and password.',
  commands: [{ command: 'wsl --install -d Ubuntu-24.04' }],
}

const dockerDesktopWindows: Step = {
  title: 'Install Docker Desktop',
  text: 'Download Docker Desktop for Windows from docker.com/products/docker-desktop and install it with the WSL 2 backend. Then in Docker Desktop: Settings → Resources → WSL integration → turn on Ubuntu-24.04 → Apply & restart.',
}

const clone: Step = {
  title: 'Get cactusInv',
  text: 'Anywhere in your home folder; on Windows in the Ubuntu window, not under /mnt/c (faster, and file permissions work). Upgrades later come from this folder.',
  commands: [{ command: `git clone ${REPO}\ncd cactusinv` }],
}

const dataFolders: Step = {
  title: 'Choose where the data lives',
  text: 'Writes .env.local beside .env (it overrides .env and upgrades leave it alone). The database, invoice PDFs, backups and secrets go in ~/cactusinv-data. Add a line TZ=Europe/London (your time zone) if it is not Africa/Johannesburg. CACTUSINV_COOKIE_SECURE=false lets you sign in over plain http; remove that line once you put HTTPS in front.',
  commands: [{
    command: [
      'cat > .env.local <<EOF',
      'DATA_DIR=$HOME/cactusinv-data',
      'MARIADB_DATA_DIR=$HOME/cactusinv-data/mariadb',
      'INVOICES_DIR=$HOME/cactusinv-data/invoices',
      'BACKUP_DIR=$HOME/cactusinv-data/backups',
      'SECRETS_DIR=$HOME/cactusinv-data/secrets',
      'CACTUSINV_COOKIE_SECURE=false',
      'EOF',
    ].join('\n'),
  }],
}

function install(target: string, extra = ''): Step {
  return {
    title: 'Install',
    text: 'Builds the image (a few minutes the first time), starts MariaDB and cactusInv, generates the secrets and prints the first sign-in password.' + extra,
    commands: [{ command: `./deploy.sh --install --${target}` }],
  }
}

function signIn(where: string): Step {
  return {
    title: 'Sign in',
    text: `Open ${where}, sign in as admin with the password the install printed, and choose your own. E-mails go to a test mailbox at http://localhost:8025 until you set your mail server in Settings → E-mail server.`,
  }
}

const keepKeyLocal: Step = {
  title: 'Keep the backup key safe',
  text: 'Copy it to a password manager or another computer: without it the encrypted backups cannot be restored.',
  commands: [{ command: 'cat ~/cactusinv-data/secrets/backup_key' }],
}

function later(target: string, ssh = ''): Step {
  return {
    title: 'Later: upgrade and look after it',
    text: 'In the cactusinv folder. An upgrade shows what changes, takes a backup first and goes back if the new version does not start.',
    commands: [
      { label: 'Upgrade to the newest version', command: `./deploy.sh --upgrade --${target}${ssh}` },
      { label: 'Take a backup now', command: `./deploy.sh --backup-now --${target}${ssh}` },
      { label: 'What is running', command: `./deploy.sh --status --${target}${ssh}` },
    ],
  }
}

const dockerLinux: Step[] = [
  {
    title: 'Install Docker Engine and git',
    text: 'Ubuntu, Debian, Fedora, RHEL, Rocky and Alma Linux (or install Docker Desktop for Linux). Then let your user run docker; log out and in again if docker still says permission denied.',
    commands: [
      { command: 'curl -fsSL https://get.docker.com | sudo sh' },
      { label: 'git: Ubuntu and Debian', command: 'sudo apt install -y git' },
      { label: 'git: Fedora, RHEL, Rocky and Alma', command: 'sudo dnf install -y git' },
      { command: 'sudo usermod -aG docker $USER\nnewgrp docker\ndocker compose version' },
    ],
  },
  clone, dataFolders, install('dockerdesktop', ' Docker starts it again after a restart.'),
  signIn('http://localhost:8080 (from another computer: http://<this-computer>:8080)'),
  keepKeyLocal, later('dockerdesktop'),
]

const dockerWindows: Step[] = [
  wsl, dockerDesktopWindows,
  {
    title: 'Install git and check Docker',
    text: 'In the Ubuntu window.',
    commands: [{ command: 'sudo apt update && sudo apt install -y git curl\ndocker compose version' }],
  },
  clone, dataFolders, install('dockerdesktop', ' Docker Desktop starts it again whenever Docker Desktop runs (Settings → General → Start Docker Desktop when you sign in).'),
  signIn('http://localhost:8080 in your Windows browser'),
  keepKeyLocal, later('dockerdesktop'),
]

const dockerMac: Step[] = [
  {
    title: 'Install Docker Desktop',
    text: 'Download Docker Desktop for Mac (Apple Silicon or Intel) from docker.com/products/docker-desktop, install and start it. Then, in Terminal, install git (skip it if git --version works).',
    commands: [{ command: 'xcode-select --install\ndocker compose version' }],
  },
  clone, dataFolders, install('dockerdesktop', ' Docker Desktop starts it again whenever Docker Desktop runs.'),
  signIn('http://localhost:8080'),
  keepKeyLocal, later('dockerdesktop'),
]

const podmanSocket: Step = {
  title: 'Start the Podman socket',
  text: 'Compose talks to Podman through its socket. Linger keeps cactusInv running when you log out; podman-restart starts it again after a reboot (Podman 5 or newer; with older Podman run the install command again after a reboot, the data is kept). Rootless: no sudo for Podman itself.',
  commands: [{ command: 'systemctl --user enable --now podman.socket podman-restart.service\nsudo loginctl enable-linger $USER\npodman compose version' }],
}

const podmanLinux: Step[] = [
  {
    title: 'Install Podman, Compose and git',
    text: 'podman compose runs docker-compose (v2), which is a separate package. On RHEL, Rocky and Alma Linux it is not packaged: download it as below.',
    commands: [
      { label: 'Fedora', command: 'sudo dnf install -y podman docker-compose git' },
      { label: 'Ubuntu 24.04 or newer', command: 'sudo apt update && sudo apt install -y podman docker-compose-v2 git' },
      {
        label: 'RHEL, Rocky and Alma Linux 9 or newer',
        command: [
          'sudo dnf install -y podman git',
          'sudo mkdir -p /usr/local/lib/docker/cli-plugins',
          'sudo curl -fL -o /usr/local/lib/docker/cli-plugins/docker-compose https://github.com/docker/compose/releases/latest/download/docker-compose-linux-$(uname -m)',
          'sudo chmod +x /usr/local/lib/docker/cli-plugins/docker-compose',
        ].join('\n'),
      },
    ],
  },
  podmanSocket, clone, dataFolders, install('podman'),
  signIn('http://localhost:8080 (from another computer: http://<this-computer>:8080)'),
  { ...keepKeyLocal, text: `${keepKeyLocal.text ?? ''} The backups folder belongs to the container's user; list it with podman unshare ls ~/cactusinv-data/backups.` },
  later('podman'),
]

const podmanWindows: Step[] = [
  wsl,
  {
    title: 'Install Podman, Compose and git',
    text: 'In the Ubuntu window.',
    commands: [{ command: 'sudo apt update && sudo apt install -y podman docker-compose-v2 git' }],
  },
  {
    ...podmanSocket,
    text: 'Compose talks to Podman through its socket (Ubuntu on WSL runs systemd). cactusInv runs while Ubuntu runs: after restarting Windows, open Ubuntu and run the install command again (the data is kept).',
    commands: [{ command: 'systemctl --user enable --now podman.socket\npodman compose version' }],
  },
  clone, dataFolders, install('podman'),
  signIn('http://localhost:8080 in your Windows browser'),
  keepKeyLocal, later('podman'),
]

const k3sKey = "kubectl -n cactusinv get secret cactusinv-secrets -o jsonpath='{.data.backup_key}' | base64 -d"

const k3sLinux: Step[] = [
  {
    title: 'Install k3s, Docker and git on the server',
    text: 'As root (sudo -i) on the Linux server. Docker builds the image; k3s gets it without a registry. The .env defaults are for k3s: volumes in /data/k3s/local.volumes/cactusinv, HTTPS on port 443 through k3s\'s Traefik.',
    commands: [
      { command: 'curl -sfL https://get.k3s.io | sh -\nk3s kubectl get nodes' },
      { command: 'curl -fsSL https://get.docker.com | sh' },
      { label: 'git: Ubuntu and Debian', command: 'apt install -y git' },
      { label: 'git: Fedora, RHEL, Rocky and Alma', command: 'dnf install -y git' },
    ],
  },
  clone,
  {
    title: 'Optional: your address and certificate',
    text: 'Without them cactusInv answers on https://<server-ip>/ with the self-signed certificate of k3s (the browser warns once). Put your own values in .env.local; leave this step out to keep the defaults.',
    commands: [{
      command: [
        'cat > .env.local <<EOF',
        'K8S_HTTPS_HOST=invoices.example.com',
        'K8S_TLS_CERT=/root/certs/fullchain.pem',
        'K8S_TLS_KEY=/root/certs/privkey.pem',
        'TZ=Africa/Johannesburg',
        'EOF',
      ].join('\n'),
    }],
  },
  install('k3s', ' Or run the steps from your own computer instead, with Docker there: ./deploy.sh --install --k3s --ssh-access root@your-server builds the image there and copies it over SSH (then the server needs only k3s).'),
  signIn('https://<server-ip>/ (or your K8S_HTTPS_HOST)'),
  { title: 'Keep the backup key safe', text: 'Copy it to a password manager or another computer: without it the encrypted backups cannot be restored.', commands: [{ command: `k3s ${k3sKey}` }] },
  later('k3s'),
]

const k3sWindows: Step[] = [
  {
    title: 'Install k3s on a Linux server',
    text: 'k3s runs on Linux only: a server, a VM or a Raspberry Pi 4/5. As root on that server:',
    commands: [{ command: 'curl -sfL https://get.k3s.io | sh -\nk3s kubectl get nodes' }],
  },
  wsl, dockerDesktopWindows,
  {
    title: 'Install git and sign in to the server with a key',
    text: 'In the Ubuntu window; replace your-server with the server\'s address. Docker on Windows builds the image, and deploy.sh copies it to the server over SSH.',
    commands: [{ command: 'sudo apt update && sudo apt install -y git openssh-client\nssh-keygen -t ed25519\nssh-copy-id root@your-server\nssh root@your-server k3s kubectl get nodes' }],
  },
  clone,
  install('k3s --ssh-access root@your-server', ' The .env defaults are for k3s: volumes in /data/k3s/local.volumes/cactusinv on the server, HTTPS on port 443 through k3s\'s Traefik. Set K8S_HTTPS_HOST, K8S_TLS_CERT and K8S_TLS_KEY in .env.local for your own address and certificate.'),
  signIn('https://your-server/ (the browser warns once about k3s\'s self-signed certificate)'),
  { title: 'Keep the backup key safe', text: 'Copy it to a password manager or another computer: without it the encrypted backups cannot be restored.', commands: [{ command: `ssh root@your-server "k3s ${k3sKey}"` }] },
  later('k3s', ' --ssh-access root@your-server'),
]

function k8sSteps(windows: boolean): Step[] {
  return [
    ...(windows ? [wsl, dockerDesktopWindows] : []),
    {
      title: 'Prepare kubectl, Docker and a registry',
      text: (windows ? 'In the Ubuntu window. ' : '') +
        'You need kubectl with your cluster\'s config (~/.kube/config; kubectl get nodes works), Docker to build the image (get.docker.com on Linux), git, and a container registry the cluster can pull from. ' +
        'The image is built for this computer\'s processor (amd64 or arm64): build on one like the cluster\'s nodes.',
      commands: [{ command: (windows ? 'sudo apt update && sudo apt install -y git\n' : '') + 'kubectl get nodes\ndocker login registry.example.com' }],
    },
    clone,
    {
      title: 'Settings for your cluster',
      text: 'In .env.local (it overrides .env). Empty K8S_STORAGE_CLASS uses the cluster\'s default storage class; empty K8S_VOLUME_DIR because folders on a node are for k3s. HTTPS goes through the cluster\'s ingress controller; without one, write K8S_HTTPS=false and CACTUSINV_COOKIE_SECURE=false and use http://<node>:30080.',
      commands: [{
        command: [
          'cat > .env.local <<EOF',
          'K8S_REGISTRY=registry.example.com/cactusinv',
          'K8S_STORAGE_CLASS=',
          'K8S_VOLUME_DIR=',
          'K8S_HTTPS_HOST=invoices.example.com',
          'TZ=Africa/Johannesburg',
          'EOF',
        ].join('\n'),
      }],
    },
    install('k8s', ' The image is pushed to K8S_REGISTRY; everything goes in namespace cactusinv.'),
    signIn('https://invoices.example.com/ (your K8S_HTTPS_HOST)'),
    { title: 'Keep the backup key safe', text: 'Copy it to a password manager or another computer: without it the encrypted backups cannot be restored.', commands: [{ command: k3sKey }] },
    later('k8s'),
  ]
}

const steps: Record<ContainerPlatform, Partial<Record<HostOS, Step[]>>> = {
  docker: { linux: dockerLinux, windows: dockerWindows, macos: dockerMac },
  podman: { linux: podmanLinux, windows: podmanWindows },
  k3s: { linux: k3sLinux, windows: k3sWindows },
  k8s: { linux: k8sSteps(false), windows: k8sSteps(true) },
}

/** containerSteps are the steps for a platform run from an operating system ([] when none). */
export function containerSteps(platform: ContainerPlatform, os: HostOS): Step[] {
  return steps[platform][os] ?? []
}
