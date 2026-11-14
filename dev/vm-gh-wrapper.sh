#!/bin/sh
# Prints the script make vm-gh-wrapper runs in the Lima VM over limactl's standard input (PRD #161 milestone
# 1, ADR 0029): it gives a VM created before dev/lima.yaml installed the gh wrapper the same layout a new VM
# gets, without recreating it.
#
# Usage: dev/vm-gh-wrapper.sh <dev/gh-app-token.sh> | limactl shell --workdir / <instance> sh -s
# (make vm-gh-wrapper generates the script first and sends it only when it is whole.)
#
# The script installs the wrapper whole or not at all. It is one function, called on its last line, so a
# stream cut short runs nothing: a here-document or function cut before its end is a syntax error, and
# without the last line nothing is called. Run as the VM user (who has passwordless sudo), the function:
#   1. writes the wrapper, with the real gh's path pinned and the SHIFT2EU_REAL_GH comment dropped by the
#      same sed script dev/lima.yaml applies, to a temporary file, and refuses it unless sh -n parses it and
#      its last line is the wrapper's exec "${real}" "$@";
#   2. refuses, changing nothing, when the real gh is neither in libexec nor at the bin path (a #! script
#      there is no gh to move);
#   3. copies the real gh from /usr/local/bin/gh to /usr/local/libexec/shift2eu/gh, root-owned, unless it is
#      there already;
#   4. installs the wrapper, root-owned 0755, as /usr/local/bin/.gh.new and moves it over /usr/local/bin/gh,
#      so a gh started meanwhile finds either the old file or the new one, never none or half of one;
#   5. points the git credential helper at the wrapper, and prints "vm-gh-wrapper: done", which the make
#      recipe requires.
# Running it again changes nothing. The wrapper travels inside the script on standard input, never on a
# command line. SHIFT2EU_VM_BIN_GH and SHIFT2EU_VM_LIBEXEC_GH, read in the VM, name the two paths for the
# tests (defaults /usr/local/bin/gh and /usr/local/libexec/shift2eu/gh).
set -eu

wrapper="${1:?usage: $0 <dev/gh-app-token.sh>}"
[ -f "${wrapper}" ] || {
  echo "vm-gh-wrapper: ${wrapper} is not a file" >&2
  exit 1
}
pinned="$(sed 's|^real=.*|real=/usr/local/libexec/shift2eu/gh|;/^# SHIFT2EU_REAL_GH /d' "${wrapper}")"
# The wrapper ends the here-document it travels in only at its delimiter: refuse one that holds that line.
if printf '%s\n' "${pinned}" | grep -qx 'SHIFT2EU_GH_WRAPPER'; then
  echo "vm-gh-wrapper: ${wrapper} holds the line SHIFT2EU_GH_WRAPPER, which would end the wrapper early" >&2
  exit 1
fi

cat <<'INSTALL'
vm_gh_wrapper() {
  set -eu
  bin_gh="${SHIFT2EU_VM_BIN_GH:-/usr/local/bin/gh}"
  libexec_gh="${SHIFT2EU_VM_LIBEXEC_GH:-/usr/local/libexec/shift2eu/gh}"
  new_gh="$(dirname "${bin_gh}")/.gh.new"
  tmp="$(mktemp)"
  trap 'rm -f "${tmp}"' EXIT
  cat >"${tmp}" <<'SHIFT2EU_GH_WRAPPER'
INSTALL
printf '%s\n' "${pinned}"
cat <<'INSTALL'
SHIFT2EU_GH_WRAPPER
  if ! sh -n "${tmp}"; then
    echo "vm-gh-wrapper: the gh wrapper does not parse; nothing installed" >&2
    return 1
  fi
  if [ "$(tail -n 1 "${tmp}")" != 'exec "${real}" "$@"' ]; then
    echo "vm-gh-wrapper: the gh wrapper does not end with its exec line, so it arrived cut short; nothing installed" >&2
    return 1
  fi
  if [ ! -x "${libexec_gh}" ]; then
    if [ ! -f "${bin_gh}" ] || [ "$(head -c 2 "${bin_gh}")" = '#!' ]; then
      echo "vm-gh-wrapper: ${bin_gh} is not the real gh to move to ${libexec_gh}; recreate the VM with make vm-delete vm-create" >&2
      return 1
    fi
    sudo install -D -o root -g root -m 0755 "${bin_gh}" "${libexec_gh}"
    echo "vm-gh-wrapper: moved the real gh to ${libexec_gh}"
  fi
  sudo install -D -o root -g root -m 0755 "${tmp}" "${new_gh}"
  sudo mv -f "${new_gh}" "${bin_gh}"
  echo "vm-gh-wrapper: installed the gh wrapper as ${bin_gh}"
  git config --global credential.https://github.com.helper "!${bin_gh} auth git-credential"
  echo "vm-gh-wrapper: the git credential helper runs ${bin_gh}"
  echo "vm-gh-wrapper: done"
}
vm_gh_wrapper
INSTALL
