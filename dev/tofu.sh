#!/usr/bin/env bash
# Runs the OpenTofu release infra/tofu.yaml pins, downloading it first if needed (issue #422). make
# infra-check and CI run it through dev/infra-check.sh; the maintainer runs it for init, plan and apply in
# infra/site on the admin machine (docs/dev-environment.md). Agents never run plan, apply or import.
#
# The release tarball for this platform (linux_amd64, linux_arm64 or darwin_arm64) is downloaded from
# opentofu/opentofu's GitHub release and must match its SHA-256 pin in infra/tofu.yaml before anything is
# extracted. The binary goes to bin/tofu/<version>-<os>_<arch>/tofu (git-ignored, one per worktree), and a
# binary already there is used only when it reports the pinned version. No other tofu, whatever the PATH
# holds, is ever run.
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

dir="bin/tofu/${version}-${os}_${arch}"
bin="${dir}/tofu"

if ! "${bin}" version 2>/dev/null | head -n 1 | grep -qx "OpenTofu ${version}"; then
  asset="tofu_${version#v}_${os}_${arch}.tar.gz"
  mkdir -p "${dir}"
  tmp="$(mktemp -d "${dir}/download.XXXXXX")"
  trap 'rm -rf "${tmp}"' EXIT
  echo >&2 "tofu: downloading OpenTofu ${version} for ${os}_${arch}"
  curl -fsSL --retry 3 -o "${tmp}/${asset}" \
    "https://github.com/opentofu/opentofu/releases/download/${version}/${asset}"
  if command -v sha256sum >/dev/null; then
    got="$(sha256sum "${tmp}/${asset}")"
  else
    got="$(shasum -a 256 "${tmp}/${asset}")"
  fi
  got="${got%% *}"
  if [ "${got}" != "${digest}" ]; then
    echo >&2 "tofu: ${asset} has SHA-256 ${got}, not ${key} in ${pins}: refusing to run it"
    exit 1
  fi
  tar -C "${tmp}" --no-same-owner -xzf "${tmp}/${asset}" tofu
  chmod 0755 "${tmp}/tofu"
  mv -f "${tmp}/tofu" "${bin}"
fi

exec "${bin}" "$@"
