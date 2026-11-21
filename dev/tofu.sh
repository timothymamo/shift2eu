#!/usr/bin/env bash
# Runs the OpenTofu release infra/tofu.yaml pins, downloading it first if needed (issue #422). make
# infra-check and CI run it through dev/infra-check.sh; the maintainer runs it for init, plan and apply in
# infra/site on the admin machine (docs/dev-environment.md). Agents never run plan, apply or import.
#
# The release tarball for this platform (linux_amd64, linux_arm64 or darwin_arm64) is downloaded once
# from opentofu/opentofu's GitHub release, over HTTPS only, into bin/tofu/<version>-<os>_<arch>/
# (git-ignored, one per worktree), and kept only if its SHA-256 is its pin in infra/tofu.yaml. Nothing
# extracted is trusted across runs: every run hashes the kept tarball against the pin again, extracts
# tofu into a fresh temporary directory beside it, runs that binary and deletes the directory. So a
# binary changed or replaced in bin/ is never run, and a changed tarball is refused. The pin is the
# tarball's digest, which Renovate updates with the version (github-release-attachments); a digest of the
# extracted binary would be one Renovate cannot keep current. No other tofu, whatever the PATH holds, is
# ever run. CI checks every pin against the release's cosign-signed tofu_<version>_SHA256SUMS
# (.github/workflows/infra-tools.yml).
#
# Usage: dev/tofu.sh <tofu arguments>   (from the repository root, where make runs it)
set -euo pipefail

pins=infra/tofu.yaml

# The quoted value of <key> in the pin file, or nothing.
pin() { # <key>
  sed -n "s/^$1: \"\([^\"]*\)\"\$/\1/p" "${pins}"
}

version="$(pin tofuVersion)"
if ! [[ "${version}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo >&2 "tofu: ${pins} pins no tofuVersion like v1.2.3"
  exit 1
fi

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) os="" ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) arch="" ;;
esac
case "${os}_${arch}" in
  linux_amd64) key=tofuSha256LinuxAmd64 ;;
  linux_arm64) key=tofuSha256LinuxArm64 ;;
  darwin_arm64) key=tofuSha256DarwinArm64 ;;
  *)
    echo >&2 "tofu: no pinned OpenTofu for $(uname -s)/$(uname -m); ${pins} pins linux_amd64, linux_arm64 and darwin_arm64"
    exit 1
    ;;
esac
digest="$(pin "${key}")"
if ! [[ "${digest}" =~ ^[0-9a-f]{64}$ ]]; then
  echo >&2 "tofu: ${pins} pins no SHA-256 for ${key}"
  exit 1
fi

sha256() { # <file>: prints its SHA-256
  local sum
  if command -v sha256sum >/dev/null; then
    sum="$(sha256sum "$1")"
  else
    sum="$(shasum -a 256 "$1")"
  fi
  echo "${sum%% *}"
}

dir="bin/tofu/${version}-${os}_${arch}"
asset="tofu_${version#v}_${os}_${arch}.tar.gz"
tarball="${dir}/${asset}"
mkdir -p "${dir}"
tmp="$(mktemp -d "${dir}/run.XXXXXX")"
trap 'rm -rf "${tmp}"' EXIT

if [ ! -f "${tarball}" ]; then
  echo >&2 "tofu: downloading OpenTofu ${version} for ${os}_${arch}"
  curl -fsSL --proto '=https' --tlsv1.2 --retry 3 -o "${tmp}/${asset}" \
    "https://github.com/opentofu/opentofu/releases/download/${version}/${asset}"
  got="$(sha256 "${tmp}/${asset}")"
  if [ "${got}" != "${digest}" ]; then
    echo >&2 "tofu: the downloaded ${asset} has SHA-256 ${got}, not ${key} in ${pins}: refusing to run it"
    exit 1
  fi
  mv -f "${tmp}/${asset}" "${tarball}"
fi

got="$(sha256 "${tarball}")"
if [ "${got}" != "${digest}" ]; then
  echo >&2 "tofu: ${tarball} has SHA-256 ${got}, not ${key} in ${pins}: refusing to run it; delete it to download the release again"
  exit 1
fi
tar -C "${tmp}" --no-same-owner -xzf "${tarball}" tofu
chmod 0755 "${tmp}/tofu"

# Not exec: the trap deletes the extracted binary once tofu exits. Ctrl-C reaches tofu as well (one
# process group), which stops as it always does; its exit status is this script's.
status=0
"${tmp}/tofu" "$@" || status=$?
exit "${status}"
