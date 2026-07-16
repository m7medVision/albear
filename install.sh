#!/bin/sh
# Albear installer.
#
#   curl -fsSL https://raw.githubusercontent.com/m7medVision/albear/main/install.sh | sh
#
# Installs vaultd, vault and vault-native for the current user, plus a systemd
# user unit for the daemon. Touches nothing outside $ALBEAR_INSTALL_DIR and the
# systemd user unit directory, and never needs root.
#
# Environment:
#   ALBEAR_VERSION       tag to install (default: latest release)
#   ALBEAR_INSTALL_DIR   where binaries go (default: ~/.local/bin)
#   ALBEAR_NO_SERVICE    set to skip installing the systemd user unit
set -eu

REPO="${ALBEAR_REPO:-m7medVision/albear}"
INSTALL_DIR="${ALBEAR_INSTALL_DIR:-$HOME/.local/bin}"
BINARIES="vaultd vault vault-native"

info()  { printf '  %s\n' "$1"; }
die()   { printf 'error: %s\n' "$1" >&2; exit 1; }

# Albear is Linux-only: vaultd authorizes clients via Unix socket peer
# credentials and has no macOS/Windows build. Fail loudly rather than install
# binaries that cannot work.
[ "$(uname -s)" = "Linux" ] || die "Albear only supports Linux (found $(uname -s))."

case "$(uname -m)" in
  x86_64 | amd64) ARCH=amd64 ;;
  aarch64 | arm64) ARCH=arm64 ;;
  *) die "unsupported architecture: $(uname -m) (albear ships amd64 and arm64)" ;;
esac

need() { command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"; }
need tar
need install
if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL "$1" -o "$2"; }
  fetch_stdout() { curl -fsSL "$1"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -qO "$2" "$1"; }
  fetch_stdout() { wget -qO- "$1"; }
else
  die "need curl or wget"
fi

if command -v sha256sum >/dev/null 2>&1; then
  sha256() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
  sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
  die "need sha256sum or shasum to verify the download"
fi

TAG="${ALBEAR_VERSION:-}"
if [ -z "$TAG" ]; then
  info "Resolving latest release..."
  # /releases/latest excludes drafts and prereleases, so this never selects a
  # half-published or -rc tag.
  TAG=$(fetch_stdout "https://api.github.com/repos/$REPO/releases/latest" |
    sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)
  [ -n "$TAG" ] || die "could not resolve the latest release (set ALBEAR_VERSION to install a specific tag)"
fi

ASSET="albear_${TAG}_linux_${ARCH}.tar.gz"
BASE="https://github.com/$REPO/releases/download/$TAG"

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT INT TERM

info "Downloading $ASSET ($TAG)..."
fetch "$BASE/$ASSET" "$TMP/$ASSET" || die "download failed: $BASE/$ASSET"
fetch "$BASE/checksums.txt" "$TMP/checksums.txt" || die "could not download checksums.txt"

info "Verifying checksum..."
want=$(grep " ${ASSET}\$" "$TMP/checksums.txt" | cut -d' ' -f1)
[ -n "$want" ] || die "$ASSET is not listed in checksums.txt"
got=$(sha256 "$TMP/$ASSET")
[ "$want" = "$got" ] || die "checksum mismatch for $ASSET (expected $want, got $got)"

tar -xzf "$TMP/$ASSET" -C "$TMP"

mkdir -p "$INSTALL_DIR"
for b in $BINARIES; do
  [ -f "$TMP/$b" ] || die "$b missing from $ASSET"
  # install(1) replaces the file rather than writing in place, so upgrading
  # while the daemon is running does not corrupt the running image.
  install -m 0755 "$TMP/$b" "$INSTALL_DIR/$b"
done
info "Installed $BINARIES to $INSTALL_DIR"

UNIT_INSTALLED=no
if [ -z "${ALBEAR_NO_SERVICE:-}" ] && command -v systemctl >/dev/null 2>&1; then
  UNIT_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
  mkdir -p "$UNIT_DIR"
  # A user unit, not a system one: vaultd checks the socket peer's uid against
  # its own and owns a per-user socket under $XDG_RUNTIME_DIR. The packaged
  # unit hardcodes /usr/bin/vaultd, so point it at this install instead.
  sed "s|^ExecStart=.*|ExecStart=$INSTALL_DIR/vaultd|" \
    "$TMP/deploy/albear-vaultd.service" >"$UNIT_DIR/albear-vaultd.service"
  systemctl --user daemon-reload >/dev/null 2>&1 || true
  UNIT_INSTALLED=yes
  info "Installed systemd user unit to $UNIT_DIR"
fi

printf '\nAlbear %s installed.\n\n' "$TAG"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) printf 'Add %s to your PATH:\n    export PATH="%s:$PATH"\n\n' "$INSTALL_DIR" "$INSTALL_DIR" ;;
esac

printf 'Next steps:\n'
if [ "$UNIT_INSTALLED" = yes ]; then
  printf '    systemctl --user enable --now albear-vaultd   # start the daemon\n'
else
  printf '    vaultd &                                      # start the daemon\n'
fi
printf '    vault init                                    # create the vault\n'
printf '    vault install chrome                          # wire up the extension\n\n'
printf 'The vault has no recovery path without a backup. Keep the master password safe.\n'
