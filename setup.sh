#!/bin/sh
# cactusInv setup: downloads the newest release of the cactusinv program for this computer
# (Linux or macOS, Intel/AMD or ARM 64-bit) into the current folder, checks it, and replaces
# an older cactusinv there (kept as cactusinv.old). Run it in the folder cactusinv lives in:
#
#   curl -fsSL https://raw.githubusercontent.com/albievan/cactusinv/main/setup.sh | sh
#
# Set CACTUSINV_VERSION=1.28.0 to fetch that release instead of the newest one.
set -eu

repo="https://github.com/albievan/cactusinv/releases"
# The public half of the release key (internal/adapters/selfupdate/releasekey.go): SHA256SUMS
# is signed with the private half by make dist.
pubkey="dltAEL/V6LB6pq64TI2PHlwSdnfN8bsExjGjRAlWdy0="

fail() { echo "cactusinv setup: $*" >&2; exit 1; }

case "$(uname -s)" in
Linux) os=linux ;;
Darwin) os=darwin ;;
*) fail "this script is for Linux and macOS; on Windows use setup.ps1 (see the README)" ;;
esac
case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) fail "no release for the $(uname -m) processor (there are amd64 and arm64)" ;;
esac
if [ "$os" = darwin ] && [ "$arch" = amd64 ] && [ "$(sysctl -n sysctl.proc_translated 2>/dev/null || echo 0)" = 1 ]; then
	arch=arm64 # an Intel shell under Rosetta on Apple Silicon: take the native program
fi
asset="cactusinv-$os-$arch"
if [ -n "${CACTUSINV_VERSION:-}" ]; then
	base="$repo/download/v$CACTUSINV_VERSION"
else
	base="$repo/latest/download"
fi

command -v curl >/dev/null 2>&1 || fail "curl is needed"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM

echo "Downloading $asset ..."
curl -fL --progress-bar -o "$tmp/$asset" "$base/$asset" || fail "download of $base/$asset failed"
curl -fsSL -o "$tmp/SHA256SUMS" "$base/SHA256SUMS" || fail "download of SHA256SUMS failed"
curl -fsSL -o "$tmp/SHA256SUMS.sig" "$base/SHA256SUMS.sig" || fail "download of SHA256SUMS.sig failed"

# The checksum list must carry the release signature. OpenSSL 3 checks it; without it (e.g.
# the LibreSSL of macOS) the script says so and relies on the checksum over HTTPS alone.
if openssl version 2>/dev/null | grep -q '^OpenSSL [3-9]'; then
	# An Ed25519 public key in DER: the fixed 12-byte prefix, then the 32 key bytes.
	{ printf '\060\052\060\005\006\003\053\145\160\003\041\000'; printf '%s' "$pubkey" | openssl base64 -d -A; } >"$tmp/key.der"
	openssl base64 -d -A <"$tmp/SHA256SUMS.sig" >"$tmp/sig.bin"
	openssl pkeyutl -verify -pubin -keyform DER -inkey "$tmp/key.der" -rawin -in "$tmp/SHA256SUMS" -sigfile "$tmp/sig.bin" >/dev/null 2>&1 ||
		fail "the release signature does not match: not installing"
	echo "Release signature: valid."
else
	echo "Release signature: not checked (needs OpenSSL 3); checking the checksum only."
fi

want="$(awk -v f="$asset" '$2 == f { print $1 }' "$tmp/SHA256SUMS")"
[ -n "$want" ] || fail "$asset is not in SHA256SUMS"
if command -v sha256sum >/dev/null 2>&1; then
	got="$(sha256sum "$tmp/$asset" | awk '{ print $1 }')"
else
	got="$(shasum -a 256 "$tmp/$asset" | awk '{ print $1 }')"
fi
[ "$got" = "$want" ] || fail "the checksum of $asset does not match: not installing"
echo "Checksum: matches."

chmod +x "$tmp/$asset"
new="$("$tmp/$asset" version)" || fail "the downloaded program does not run on this computer"
if [ -f cactusinv ]; then
	mv -f cactusinv cactusinv.old
	echo "Kept the previous program as cactusinv.old ($(./cactusinv.old version 2>/dev/null || echo unknown version))."
fi
mv "$tmp/$asset" cactusinv
echo "Installed cactusinv $new in $(pwd)."
echo "Restart cactusinv if it was running. A first install needs a MariaDB 11 database and a"
echo "db_password secret: see https://github.com/albievan/cactusinv#installing-the-program"
