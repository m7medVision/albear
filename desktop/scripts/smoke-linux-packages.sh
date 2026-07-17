#!/usr/bin/env bash
set -euo pipefail

build_dir=${1:?usage: smoke-linux-packages.sh BUILD_DIR CORE_DIR TAG}
core_dir=${2:?usage: smoke-linux-packages.sh BUILD_DIR CORE_DIR TAG}
tag=${3:?usage: smoke-linux-packages.sh BUILD_DIR CORE_DIR TAG}
version=${tag#v}

command -v docker >/dev/null || {
  echo 'docker is required for clean distribution smoke tests' >&2
  exit 1
}

desktop_deb="$build_dir/albear-desktop_${version}_amd64.deb"
desktop_rpm="$build_dir/albear-desktop_${version}_x86_64.rpm"
core_deb=$(find "$core_dir" -maxdepth 1 -type f -name 'albear_*_linux_amd64.deb' | head -1)
core_rpm=$(find "$core_dir" -maxdepth 1 -type f -name 'albear_*_linux_amd64.rpm' | head -1)

for artifact in "$desktop_deb" "$desktop_rpm" "$core_deb" "$core_rpm"; do
  test -s "$artifact" || {
    echo "smoke-test package missing or empty: $artifact" >&2
    exit 1
  }
done

staging=$(mktemp -d)
trap 'rm -rf "$staging"' EXIT
install -m 0644 "$core_deb" "$staging/albear.deb"
install -m 0644 "$desktop_deb" "$staging/albear-desktop.deb"
install -m 0644 "$core_rpm" "$staging/albear.rpm"
install -m 0644 "$desktop_rpm" "$staging/albear-desktop.rpm"

# Installing both local files in one invocation proves the desktop dependency
# can be satisfied without pretending GitHub Releases is an apt repository.
docker run --rm -e DEBIAN_FRONTEND=noninteractive -v "$staging:/packages:ro" ubuntu:24.04 bash -euc '
  apt-get -qq update >/tmp/apt.log 2>&1 || { cat /tmp/apt.log; exit 1; }
  apt-get -qq install -y /packages/albear.deb /packages/albear-desktop.deb >>/tmp/apt.log 2>&1 || { cat /tmp/apt.log; exit 1; }
  command -v vault vaultd vault-native
  test -x /opt/Albear/albear-desktop
  test -f /usr/lib/systemd/user/albear-vaultd.service
  test -f /usr/share/applications/dev.albear.desktop
  dpkg-query -W -f="\${Status}\n" albear albear-desktop | grep -c "install ok installed" | grep -qx 2
  apt-get -qq remove -y albear-desktop albear >>/tmp/apt.log 2>&1 || { cat /tmp/apt.log; exit 1; }
'

docker run --rm -v "$staging:/packages:ro" fedora:latest bash -euc '
  dnf -q install -y /packages/albear.rpm /packages/albear-desktop.rpm >/tmp/dnf.log 2>&1 || { cat /tmp/dnf.log; exit 1; }
  command -v vault vaultd vault-native
  test -x /opt/Albear/albear-desktop
  test -f /usr/lib/systemd/user/albear-vaultd.service
  test -f /usr/share/applications/dev.albear.desktop
  rpm -q albear albear-desktop
  dnf -q remove -y albear-desktop albear >>/tmp/dnf.log 2>&1 || { cat /tmp/dnf.log; exit 1; }
'

printf 'Ubuntu and Fedora package smoke tests passed for %s\n' "$tag"
