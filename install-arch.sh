#!/bin/sh
# Albear installer for Arch Linux.
#
#   curl -fsSL https://raw.githubusercontent.com/m7medVision/albear/main/install-arch.sh | sh
#
# Installs the albear-bin package (daemon, CLI, native relay, systemd user
# unit, browser extension and, on x86_64, the desktop app), built on this
# machine from albear's latest stable release tag. Read it first if you like:
# this script and packaging/arch/PKGBUILD are all that runs.
#
# albear is NOT on the AUR: new AUR account registration is closed. With paru
# installed, this script registers albear's own PKGBUILD repository in your
# user paru config, so `paru -Syu` keeps albear updated like any AUR package.
# Without paru it builds the PKGBUILD once with makepkg, and updates are
# manual (re-run this script).
#
# Never run as root: root is only ever used through the sudo prompts that
# pacman, paru and makepkg make themselves.
#
# Environment:
#   ALBEAR_ARCH_DRY_RUN  set to only write the paru config and print every
#                        other step instead of running it (used by the tests)
set -eu

REPO_URL="https://github.com/m7medVision/albear"
PKG="albear-bin"
PKGBUILD_PATH="packaging/arch"

say() { printf '%s\n' "$*"; }
warn() { printf 'warning: %s\n' "$*" >&2; }
die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

dry_run() { [ -n "${ALBEAR_ARCH_DRY_RUN:-}" ]; }

# run CMD... runs a step, or only prints it in a dry run. Under `curl | sh`
# stdin is the script itself, so interactive steps (sudo, paru's review,
# makepkg's prompts) read from the terminal instead when there is one.
run() {
  if dry_run; then
    say "[dry run] $*"
    return 0
  fi
  if (: </dev/tty) 2>/dev/null; then
    "$@" </dev/tty
  else
    "$@"
  fi
}

aur_warning() {
  say ""
  say "Note: albear is not on the AUR, because new AUR account registration is"
  say "closed. It installs from albear's own PKGBUILD repository instead:"
  say "  $REPO_URL/tree/main/$PKGBUILD_PATH"
  say ""
}

check_system() {
  if [ ! -f /etc/arch-release ] && ! command -v pacman >/dev/null 2>&1; then
    die "this installer is for Arch Linux. On other distributions use:
  curl -fsSL https://raw.githubusercontent.com/m7medVision/albear/main/install.sh | sh"
  fi
  if [ "$(id -u)" -eq 0 ]; then
    die "do not run this as root; run it as your user (makepkg refuses root, and the daemon is a per-user service)"
  fi
  [ -n "${HOME:-}" ] || die "HOME is not set"
}

# paru_conf prints the user paru config paru will read: $PARU_CONF, else the
# first existing of $XDG_CONFIG_HOME/paru/paru.conf and ~/.config/paru/paru.conf
# (paru's own lookup order), else where a new one should go.
paru_conf() {
  if [ -n "${PARU_CONF:-}" ]; then
    printf '%s\n' "$PARU_CONF"
    return
  fi
  if [ -n "${XDG_CONFIG_HOME:-}" ] && [ -f "$XDG_CONFIG_HOME/paru/paru.conf" ]; then
    printf '%s\n' "$XDG_CONFIG_HOME/paru/paru.conf"
    return
  fi
  if [ -f "$HOME/.config/paru/paru.conf" ]; then
    printf '%s\n' "$HOME/.config/paru/paru.conf"
    return
  fi
  printf '%s\n' "${XDG_CONFIG_HOME:-$HOME/.config}/paru/paru.conf"
}

# configure_paru registers albear's PKGBUILD repository and turns on Devel,
# without touching anything else in the config. Safe to run repeatedly.
configure_paru() {
  conf=$(paru_conf)
  if [ ! -f "$conf" ]; then
    mkdir -p "$(dirname "$conf")"
    # paru reads only the first config it finds, so a new user config must
    # pull in the system one or every system-wide setting would be lost.
    if [ -f /etc/paru.conf ]; then
      printf 'Include = /etc/paru.conf\n' >"$conf"
    else
      : >"$conf"
    fi
    say "Created $conf"
  fi

  # Devel makes `paru -Syu` notice new albear releases (see the PKGBUILD).
  # An explicit [options] header keeps it out of whatever section ends the
  # file, including the Include'd system config.
  if ! grep -Eq '^[[:space:]]*Devel[[:space:]]*$' "$conf"; then
    printf '\n[options]\nDevel\n' >>"$conf"
    say "Enabled Devel in $conf"
  fi

  if grep -Eq '^[[:space:]]*\[albear\][[:space:]]*$' "$conf"; then
    say "albear's PKGBUILD repository is already in $conf"
  else
    printf '\n[albear]\nUrl = %s\nPath = %s\n' "$REPO_URL" "$PKGBUILD_PATH" >>"$conf"
    say "Added albear's PKGBUILD repository to $conf"
  fi
}

install_with_paru() {
  configure_paru
  run paru -Sya
  if pacman -Q "$PKG" >/dev/null 2>&1; then
    say "$PKG is already installed; \`paru -Syu\` updates it."
  else
    run paru -S --needed "$PKG"
  fi
}

install_with_makepkg() {
  if ! command -v git >/dev/null 2>&1 || ! command -v makepkg >/dev/null 2>&1; then
    die "git and makepkg are required: sudo pacman -S --needed git base-devel"
  fi
  dir="${XDG_CACHE_HOME:-$HOME/.cache}/albear/arch"
  if [ -d "$dir/.git" ]; then
    run git -C "$dir" pull --ff-only
  else
    run git clone --depth 1 "$REPO_URL.git" "$dir"
  fi
  if dry_run; then
    say "[dry run] cd $dir/$PKGBUILD_PATH && makepkg -si"
  else
    (cd "$dir/$PKGBUILD_PATH" && run makepkg -si)
  fi
  warn "paru was not found, so albear will NOT update automatically."
  warn "Re-run this installer to update, or install paru and re-run it once to"
  warn "get updates through \`paru -Syu\`:"
  warn "  https://github.com/Morganamilo/paru#installation"
}

main() {
  check_system
  aur_warning

  if command -v paru >/dev/null 2>&1; then
    install_with_paru
  else
    install_with_makepkg
  fi

  if ! run systemctl --user enable --now albear-vaultd; then
    warn "could not start the albear-vaultd user service; start it later with:"
    warn "  systemctl --user enable --now albear-vaultd"
  fi
  # Every supported browser that has a config folder: Chromium, Brave and
  # Helium install the extension on their next start, Chrome through the
  # system file the package installs.
  if ! run vault install; then
    warn "\`vault install\` failed; run it again once the problem above is fixed"
  fi

  aur_warning
  say "albear is installed. Next step: create your vault with \`vault init\`,"
  say "or open Albear from your application menu. Restart your browser to load"
  say "the extension."
}

main "$@"
