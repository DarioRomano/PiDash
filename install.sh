#!/bin/bash
# PiDash installer and updater.
#
# Install or update to the latest release on a Raspberry Pi running Raspberry
# Pi OS with desktop, as your normal user:
#
#   curl -fsSL https://raw.githubusercontent.com/DarioRomano/PiDash/main/install.sh | bash
#
# With options (see deploy/install.sh for what they do):
#
#   curl -fsSL https://raw.githubusercontent.com/DarioRomano/PiDash/main/install.sh | bash -s -- --autostart --phone-input
#
# It downloads the package for this machine from the GitHub release, checks it
# against the release's checksums, and runs the installer inside it. Nothing
# needs to be compiled on the Pi. Running it again updates PiDash and keeps
# your settings; options given earlier are reused unless you pass new ones.
#
# Environment:
#   PIDASH_VERSION   a release tag such as v0.2.0 (default: the latest release)
#   PIDASH_REPO      owner/name of the GitHub repository (default: DarioRomano/PiDash)
#   PIDASH_BASE_URL  download from this address instead of GitHub (mirrors, testing)
set -euo pipefail

# Everything is inside main, called on the last line, so that a download cut
# off halfway cannot run half a script.
main() {
  local repo="${PIDASH_REPO:-DarioRomano/PiDash}"
  local version="${PIDASH_VERSION:-latest}"
  local base="${PIDASH_BASE_URL:-}"

  if [ "$(id -u)" -eq 0 ]; then
    echo "Run this as your normal desktop user, without sudo." >&2
    exit 1
  fi

  local arch
  case "$(uname -m)" in
    aarch64 | arm64) arch=arm64 ;;
    armv6l | armv7l | armv8l) arch=armv6 ;;
    x86_64 | amd64) arch=amd64 ;;
    *)
      echo "PiDash has no package for this kind of machine ($(uname -m))." >&2
      exit 1
      ;;
  esac
  if [ "$(uname -s)" != "Linux" ]; then
    echo "PiDash runs on Linux (Raspberry Pi OS)." >&2
    exit 1
  fi

  if [ -z "$base" ]; then
    if [ "$version" = "latest" ]; then
      base="https://github.com/$repo/releases/latest/download"
    else
      base="https://github.com/$repo/releases/download/$version"
    fi
  fi

  if command -v curl >/dev/null 2>&1; then
    fetch() { curl -fsSL --retry 3 --connect-timeout 20 -o "$2" "$1"; }
  elif command -v wget >/dev/null 2>&1; then
    fetch() { wget -q --tries=3 -O "$2" "$1"; }
  else
    echo "Neither curl nor wget is installed: sudo apt install curl" >&2
    exit 1
  fi

  local tmp package="pidash_linux_$arch.tar.gz"
  tmp="$(mktemp -d)"
  # shellcheck disable=SC2064  # expand $tmp now: it is local to this function
  trap "rm -rf '$tmp'" EXIT

  echo "==> Downloading PiDash ($version, $arch)"
  if ! fetch "$base/$package" "$tmp/$package" || ! fetch "$base/SHA256SUMS" "$tmp/SHA256SUMS"; then
    echo "Download failed from $base" >&2
    echo "Check the internet connection, and that the release exists: https://github.com/$repo/releases" >&2
    exit 1
  fi

  echo "==> Checking the download"
  if ! (cd "$tmp" && grep " $package\$" SHA256SUMS | sha256sum -c --status -); then
    echo "The downloaded package does not match its checksum. Nothing was installed." >&2
    exit 1
  fi

  tar -C "$tmp" -xzf "$tmp/$package"
  local dir="$tmp/pidash_linux_$arch"
  echo "==> Installing PiDash $(cat "$dir/VERSION" 2>/dev/null || echo '?')"

  # Options chosen at install time are kept; new ones are added to them.
  local flags=("$@") saved="$HOME/.local/share/pidash/install-flags" earlier f have
  if [ -s "$saved" ]; then
    mapfile -t earlier < "$saved"
    for f in "${earlier[@]}"; do
      have=0
      for g in "${flags[@]}"; do [ "$g" = "$f" ] && have=1; done
      [ "$have" -eq 1 ] || flags+=("$f")
    done
    [ ${#earlier[@]} -eq 0 ] || echo "    keeping options: ${earlier[*]}"
  fi

  bash "$dir/deploy/install.sh" "${flags[@]}"
}

main "$@"
