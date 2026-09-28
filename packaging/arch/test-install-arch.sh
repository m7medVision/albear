#!/bin/sh
# Tests install-arch.sh's paru config editing against a throwaway HOME and
# XDG_CONFIG_HOME. ALBEAR_ARCH_DRY_RUN stops the installer before paru,
# makepkg, systemctl or vault would run, and a stub paru on PATH fails the
# test if anything calls it anyway. Never touches the real ~/.config/paru.
#
#   sh packaging/arch/test-install-arch.sh
set -eu

root=$(cd "$(dirname "$0")/../.." && pwd)
script="$root/install-arch.sh"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

mkdir -p "$tmp/bin"
cat >"$tmp/bin/paru" <<'EOF'
#!/bin/sh
echo "stub paru called during a dry run: $*" >&2
exit 99
EOF
chmod +x "$tmp/bin/paru"

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

# install NAME: runs the installer in dry-run mode with a fresh HOME per case.
install() {
  HOME="$tmp/$1/home" XDG_CONFIG_HOME="$tmp/$1/config" PATH="$tmp/bin:$PATH" \
    ALBEAR_ARCH_DRY_RUN=1 PARU_CONF='' sh "$script" >"$tmp/$1.out" 2>&1 ||
    fail "$1: installer exited non-zero: $(cat "$tmp/$1.out")"
}

count() { grep -Ec "$1" "$2" || true; }

# A user with only the system paru config gets a user config that includes it.
conf="$tmp/new/config/paru/paru.conf"
install new
install new
[ "$(head -n 1 "$conf")" = "Include = /etc/paru.conf" ] ||
  [ ! -f /etc/paru.conf ] || fail "new: first line is not the Include: $(head -n 1 "$conf")"
[ "$(count '^\[albear\]$' "$conf")" = 1 ] || fail "new: want one [albear] section: $(cat "$conf")"
[ "$(count '^Devel$' "$conf")" = 1 ] || fail "new: want one Devel: $(cat "$conf")"
grep -qx 'Url = https://github.com/m7medVision/albear' "$conf" || fail "new: no Url"
grep -qx 'Path = packaging/arch' "$conf" || fail "new: no Path"
# Devel must sit under an [options] header, never inside [albear].
awk '/^\[/{s=$0} /^Devel$/{print s}' "$conf" | grep -qx '\[options\]' ||
  fail "new: Devel is not in [options]: $(cat "$conf")"
grep -q 'not on the AUR' "$tmp/new.out" || fail "new: no AUR-closed warning"
grep -q '\[dry run\] paru -S --needed albear-bin' "$tmp/new.out" ||
  fail "new: install step missing: $(cat "$tmp/new.out")"

# An existing user config keeps its settings and gains the section once.
conf="$tmp/existing/config/paru/paru.conf"
mkdir -p "$(dirname "$conf")"
printf '[options]\nBottomUp\nCleanAfter\n\n[mine]\nUrl = https://example.com/pkgbuilds\n' >"$conf"
cp "$conf" "$tmp/existing.orig"
install existing
install existing
head -n 6 "$conf" | cmp -s - "$tmp/existing.orig" || fail "existing: original settings changed: $(cat "$conf")"
[ "$(count '^\[albear\]$' "$conf")" = 1 ] || fail "existing: want one [albear] section: $(cat "$conf")"
[ "$(count '^Devel$' "$conf")" = 1 ] || fail "existing: want one Devel: $(cat "$conf")"

# A config that already has Devel gets no second [options] section.
conf="$tmp/devel/config/paru/paru.conf"
mkdir -p "$(dirname "$conf")"
printf 'Include = /etc/paru.conf\n\n[options]\nDevel\n' >"$conf"
install devel
[ "$(count '^\[options\]$' "$conf")" = 1 ] || fail "devel: added a second [options]: $(cat "$conf")"
[ "$(count '^\[albear\]$' "$conf")" = 1 ] || fail "devel: want one [albear] section"

# paru's lookup order: an existing ~/.config/paru/paru.conf wins over a
# missing $XDG_CONFIG_HOME/paru/paru.conf, so that is the file to edit.
mkdir -p "$tmp/fallback/home/.config/paru"
printf '[options]\nBottomUp\n' >"$tmp/fallback/home/.config/paru/paru.conf"
install fallback
[ ! -e "$tmp/fallback/config/paru/paru.conf" ] || fail "fallback: created a config paru would not read"
grep -q '^\[albear\]$' "$tmp/fallback/home/.config/paru/paru.conf" || fail "fallback: section not added"

echo "install-arch.sh config tests passed"
