package selfupdate

// releasePublicKey is the public half of the release key (M45, base64 Ed25519): a release's
// SHA256SUMS must carry a SHA256SUMS.sig made with the private half (tools/releasesign,
// run by make dist), or the installer refuses it. The private key is kept off the
// repository by the owner.
const releasePublicKey = "dltAEL/V6LB6pq64TI2PHlwSdnfN8bsExjGjRAlWdy0="
