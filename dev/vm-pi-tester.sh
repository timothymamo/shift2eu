#!/bin/sh
# Prints the script make vm-pi-tester runs in the Lima VM over limactl's standard input (issue #692,
# docs/adr/0044-refuse-printing-the-environment-in-pi-with-a-project-extension.md): it installs the tester's
# launch wrapper and the env-printing refusal it loads, root-owned, in /usr/local/lib/shift2eu/pi-tester, as
# dev/lima.yaml does for a new VM, without recreating the VM. The maintainer runs it from the host clone after
# a merge that changes dev/pi-tester.sh, .pi/extensions/deny-env-printing.ts or .pi/deny-env-printing.json;
# agents never do.
#
# Usage: dev/vm-pi-tester.sh <dev/pi-tester.sh> <.pi/extensions/deny-env-printing.ts> <.pi/deny-env-printing.json> |
#          limactl shell --workdir / <instance> sh -s
# (make vm-pi-tester generates the script first and sends it only when it is whole.)
#
# The script installs the three files whole or not at all. It is one function, called on its last line, so a
# stream cut short runs nothing: a here-document or function cut before its end is a syntax error, and
# without the last line nothing is called. Each file travels byte for byte inside its own here-document, never
# on a command line; a file that holds its here-document's delimiter line, or does not end with a newline, is
# refused here, before anything is printed. Run as the VM user (who has passwordless sudo), the function:
#   1. writes the three files to a temporary directory, and refuses them unless sh -n parses the wrapper and
#      its last line is the wrapper's exec line;
#   2. creates /usr/local/lib/shift2eu/pi-tester and its extensions directory, root-owned 0755;
#   3. installs each file, root-owned (the wrapper 0755, the two refusal files 0644), as a .new file beside
#      its destination and moves it over the destination, so a tester started meanwhile finds either the old
#      file or the new one, never none or half of one;
#   4. prints "vm-pi-tester: done", which the make recipe requires.
# Running it again changes nothing.
set -eu

usage="usage: $0 <dev/pi-tester.sh> <.pi/extensions/deny-env-printing.ts> <.pi/deny-env-printing.json>"
wrapper="${1:?${usage}}"
extension="${2:?${usage}}"
list="${3:?${usage}}"

# check_file <file> <delimiter> refuses a file the here-document it travels in would not carry byte for byte.
check_file() {
  [ -f "$1" ] || {
    echo "vm-pi-tester: $1 is not a file" >&2
    exit 1
  }
  [ -s "$1" ] || {
    echo "vm-pi-tester: $1 is empty" >&2
    exit 1
  }
  if grep -qx "$2" "$1"; then
    echo "vm-pi-tester: $1 holds the line $2, which would end its here-document early" >&2
    exit 1
  fi
  if [ -n "$(tail -c 1 "$1")" ]; then
    echo "vm-pi-tester: $1 does not end with a newline, so its here-document would not carry it byte for byte" >&2
    exit 1
  fi
}

check_file "${wrapper}" SHIFT2EU_PITESTER_WRAPPER
check_file "${extension}" SHIFT2EU_PITESTER_EXTENSION
check_file "${list}" SHIFT2EU_PITESTER_LIST

cat <<'INSTALL'
vm_pi_tester() {
  set -eu
  dir=/usr/local/lib/shift2eu/pi-tester
  tmp="$(mktemp -d)"
  trap 'rm -rf "${tmp}"' EXIT
  cat >"${tmp}/pi-tester.sh" <<'SHIFT2EU_PITESTER_WRAPPER'
INSTALL
cat "${wrapper}"
cat <<'INSTALL'
SHIFT2EU_PITESTER_WRAPPER
  cat >"${tmp}/deny-env-printing.ts" <<'SHIFT2EU_PITESTER_EXTENSION'
INSTALL
cat "${extension}"
cat <<'INSTALL'
SHIFT2EU_PITESTER_EXTENSION
  cat >"${tmp}/deny-env-printing.json" <<'SHIFT2EU_PITESTER_LIST'
INSTALL
cat "${list}"
cat <<'INSTALL'
SHIFT2EU_PITESTER_LIST
  if ! sh -n "${tmp}/pi-tester.sh"; then
    echo "vm-pi-tester: the tester's wrapper does not parse; nothing installed" >&2
    return 1
  fi
  if [ "$(tail -n 1 "${tmp}/pi-tester.sh")" != 'allowlisted exec "${pi}" --extension "${extension}" "$@"' ]; then
    echo "vm-pi-tester: the tester's wrapper does not end with its exec line, so it arrived cut short; nothing installed" >&2
    return 1
  fi
  sudo install -d -o root -g root -m 0755 "${dir}" "${dir}/extensions"
  sudo install -o root -g root -m 0755 "${tmp}/pi-tester.sh" "${dir}/.pi-tester.sh.new"
  sudo mv -f "${dir}/.pi-tester.sh.new" "${dir}/pi-tester.sh"
  sudo install -o root -g root -m 0644 "${tmp}/deny-env-printing.ts" "${dir}/extensions/.deny-env-printing.ts.new"
  sudo mv -f "${dir}/extensions/.deny-env-printing.ts.new" "${dir}/extensions/deny-env-printing.ts"
  sudo install -o root -g root -m 0644 "${tmp}/deny-env-printing.json" "${dir}/.deny-env-printing.json.new"
  sudo mv -f "${dir}/.deny-env-printing.json.new" "${dir}/deny-env-printing.json"
  echo "vm-pi-tester: installed the tester's wrapper and env-printing refusal in ${dir}"
  echo "vm-pi-tester: done"
}
vm_pi_tester
INSTALL
