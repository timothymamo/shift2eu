#!/bin/sh
# Prints the script make vm-gh-wrapper runs in the Lima VM over limactl's standard input (PRD #161 milestone
# 1, ADR 0029): it gives a VM created before dev/lima.yaml installed the gh wrapper the same layout a new VM
# gets, without recreating it.
#
# Usage: dev/vm-gh-wrapper.sh <dev/gh-app-token.sh> | limactl shell --workdir / <instance> sh -s
#
# The script, run as the VM user (who has passwordless sudo):
#   1. moves the real gh from /usr/local/bin/gh to /usr/local/libexec/shift2eu/gh, root-owned, unless it is
#      there already (refusing when /usr/local/bin/gh is a script, not a gh to move);
#   2. installs the wrapper, root-owned 0755, as /usr/local/bin/gh, with the real gh's path pinned and the
#      SHIFT2EU_REAL_GH comment dropped by the same sed script dev/lima.yaml applies;
#   3. points the git credential helper at the wrapper.
# Running it again changes nothing. The wrapper travels inside the script on standard input, never on a
# command line.
set -eu

wrapper="${1:?usage: $0 <dev/gh-app-token.sh>}"
[ -f "${wrapper}" ] || {
  echo "vm-gh-wrapper: ${wrapper} is not a file" >&2
  exit 1
}

cat <<'INSTALL'
set -eu
libexec_gh=/usr/local/libexec/shift2eu/gh
bin_gh=/usr/local/bin/gh
if [ ! -x "${libexec_gh}" ]; then
  if [ ! -f "${bin_gh}" ] || [ "$(head -c 2 "${bin_gh}")" = '#!' ]; then
    echo "vm-gh-wrapper: ${bin_gh} is not the real gh to move to ${libexec_gh}; recreate the VM with make vm-delete vm-create" >&2
    exit 1
  fi
  sudo install -D -o root -g root -m 0755 "${bin_gh}" "${libexec_gh}"
  echo "vm-gh-wrapper: moved the real gh to ${libexec_gh}"
fi
sudo install -o root -g root -m 0755 /dev/stdin "${bin_gh}" <<'SHIFT2EU_GH_WRAPPER'
INSTALL
sed 's|^real=.*|real=/usr/local/libexec/shift2eu/gh|;/^# SHIFT2EU_REAL_GH /d' "${wrapper}"
cat <<'INSTALL'
SHIFT2EU_GH_WRAPPER
echo "vm-gh-wrapper: installed the gh wrapper as ${bin_gh}"
git config --global credential.https://github.com.helper '!/usr/local/bin/gh auth git-credential'
echo "vm-gh-wrapper: the git credential helper runs ${bin_gh}"
INSTALL
