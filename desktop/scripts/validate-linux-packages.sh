#!/usr/bin/env bash
set -euo pipefail

build_dir=${1:?usage: validate-linux-packages.sh BUILD_DIR TAG}
tag=${2:?usage: validate-linux-packages.sh BUILD_DIR TAG}
version=${tag#v}

appimage="$build_dir/albear-desktop_${version}_x86_64.AppImage"
deb="$build_dir/albear-desktop_${version}_amd64.deb"
rpm_pkg="$build_dir/albear-desktop_${version}_x86_64.rpm"

for tool in dpkg-deb rpm; do
  command -v "$tool" >/dev/null || {
    echo "required package inspection tool not found: $tool" >&2
    exit 1
  }
done
for artifact in "$appimage" "$deb" "$rpm_pkg"; do
  test -s "$artifact" || {
    echo "expected desktop artifact missing or empty: $artifact" >&2
    exit 1
  }
done

assert_version() {
  local actual=$1 expected=${version//-/~}
  if [[ $actual != "$expected" ]]; then
    echo "package version $actual does not match release $version (expected $expected)" >&2
    exit 1
  fi
}
assert_dependency() {
  local dependencies=$1 required=$2
  if ! grep -Eq "(^|[[:space:],])${required}([[:space:],(<=>]|$)" <<<"$dependencies"; then
    echo "missing package dependency: $required" >&2
    exit 1
  fi
}
assert_desktop_contents() {
  local contents=$1
  grep -Eq '/opt/Albear/albear-desktop$' <<<"$contents"
  grep -Eq '/usr/share/applications/.+\.desktop$' <<<"$contents"
  grep -Eq '/usr/share/icons/hicolor/.+/apps/albear-desktop\.png$' <<<"$contents"
  if grep -Eq '(^|/)(vaultd|vault|vault-native)$|albear-vaultd\.service$' <<<"$contents"; then
    echo "desktop package overlaps files owned by the core package" >&2
    exit 1
  fi
}

deb_name=$(dpkg-deb -f "$deb" Package)
deb_version=$(dpkg-deb -f "$deb" Version)
deb_arch=$(dpkg-deb -f "$deb" Architecture)
deb_depends=$(dpkg-deb -f "$deb" Depends)
[[ $deb_name == albear-desktop ]] || { echo "unexpected deb package: $deb_name" >&2; exit 1; }
[[ $deb_arch == amd64 ]] || { echo "unexpected deb architecture: $deb_arch" >&2; exit 1; }
assert_version "$deb_version"
for dep in albear libgtk-3-0 libnotify4 libnss3 libxss1 libxtst6 xdg-utils libatspi2.0-0 libuuid1 libsecret-1-0; do
  assert_dependency "$deb_depends" "$dep"
done
assert_desktop_contents "$(dpkg-deb -c "$deb")"

rpm_name=$(rpm -qp --qf '%{NAME}' "$rpm_pkg")
rpm_version=$(rpm -qp --qf '%{VERSION}' "$rpm_pkg")
rpm_arch=$(rpm -qp --qf '%{ARCH}' "$rpm_pkg")
rpm_depends=$(rpm -qpR "$rpm_pkg")
[[ $rpm_name == albear-desktop ]] || { echo "unexpected rpm package: $rpm_name" >&2; exit 1; }
[[ $rpm_arch == x86_64 ]] || { echo "unexpected rpm architecture: $rpm_arch" >&2; exit 1; }
assert_version "$rpm_version"
for dep in albear gtk3 libnotify nss libXScrnSaver xdg-utils at-spi2-core; do
  assert_dependency "$rpm_depends" "$dep"
done
assert_desktop_contents "$(rpm -qpl "$rpm_pkg")"

printf 'Validated desktop AppImage, deb and rpm for %s\n' "$tag"
