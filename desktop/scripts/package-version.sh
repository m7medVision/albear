#!/usr/bin/env bash

# electron-builder maps npm prerelease separators to `~` for deb/rpm metadata
# so prereleases sort before the corresponding stable version.
package_version_for_release() {
  local version=${1#v}
  printf '%s\n' "$version" | sed 's/-/~/g'
}
