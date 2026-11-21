#!/usr/bin/env bash
# Compares a provider lock file that `tofu providers lock` regenerated with the one git holds (issue #434),
# for the "Infra tools" workflow's verify-provider-lock job. tofu init accepts a lock file as long as it holds
# a hash for the platform it runs on, so a committed file that lost another platform's hash, or holds one
# the registry never published, would pass on the machine that changed it and fail, or pass unreviewed,
# elsewhere. Here every provider block must be in both files, with the same version, the same constraints
# and the same set of hashes, whatever the order of the blocks and of the hashes.
#
# Each difference prints one line on stderr, "infra-lock-compare: [lock-hashes] <provider>: <what>", and
# the script exits 1; equal files print nothing and exit 0; a usage error exits 2. It reads the two files
# only: no tofu, no network, no credential (bash, awk, sort and comm; tested by TestInfraLockCompare).
#
# Usage: dev/infra-lock-compare.sh <committed lock file> <regenerated lock file>
set -euo pipefail

usage() {
  echo >&2 "usage: dev/infra-lock-compare.sh <committed .terraform.lock.hcl> <regenerated .terraform.lock.hcl>"
  exit 2
}

if [ "$#" -ne 2 ]; then
  usage
fi
for f in "$1" "$2"; do
  if [ ! -f "${f}" ]; then
    echo >&2 "infra-lock-compare: ${f} is not a file"
    usage
  fi
done

# Prints one line per fact of a lock file, sorted, tab-separated: "provider <address>", "version <address>
# <version>", "constraints <address> <constraints>" and "hash <address> <hash>". A line it does not read
# (comments, the hashes = [ and ] lines) carries no fact.
facts() { # <lock file>
  LC_ALL=C awk '
    /^[[:space:]]*provider[[:space:]]*"[^"]*"[[:space:]]*\{[[:space:]]*$/ {
      addr = $0
      sub(/^[^"]*"/, "", addr)
      sub(/".*$/, "", addr)
      print "provider\t" addr
      next
    }
    addr != "" && /^[[:space:]]*(version|constraints)[[:space:]]*=/ {
      key = $0
      sub(/^[[:space:]]*/, "", key)
      sub(/[[:space:]]*=.*$/, "", key)
      val = $0
      sub(/^[^"]*"/, "", val)
      sub(/".*$/, "", val)
      print key "\t" addr "\t" val
      next
    }
    addr != "" && /^[[:space:]]*"[^"]*"[[:space:]]*,?[[:space:]]*$/ {
      h = $0
      sub(/^[^"]*"/, "", h)
      sub(/".*$/, "", h)
      print "hash\t" addr "\t" h
      next
    }
    /^}[[:space:]]*$/ { addr = "" }
  ' "$1" | LC_ALL=C sort -u
}

committed="$(facts "$1")"
regenerated="$(facts "$2")"

failed=0
# <committed|regenerated> holds a fact the other file does not.
report() { # <which file holds it> <the other file> <fact line>
  local kind addr rest
  IFS=$'\t' read -r kind addr rest <<<"$3"
  case "${kind}" in
    provider) what="a provider block only the $1 file holds" ;;
    version) what="version ${rest} only in the $1 file" ;;
    constraints) what="constraints \"${rest}\" only in the $1 file" ;;
    hash) what="hash ${rest} only in the $1 file (missing from the $2 one)" ;;
    *) what="an unexpected line: ${3}" ;;
  esac
  echo >&2 "infra-lock-compare: [lock-hashes] ${addr}: ${what}"
  failed=1
}

while IFS= read -r line; do
  [ -n "${line}" ] && report committed regenerated "${line}"
done <<<"$(LC_ALL=C comm -23 <(printf '%s\n' "${committed}") <(printf '%s\n' "${regenerated}"))"
while IFS= read -r line; do
  [ -n "${line}" ] && report regenerated committed "${line}"
done <<<"$(LC_ALL=C comm -13 <(printf '%s\n' "${committed}") <(printf '%s\n' "${regenerated}"))"

exit "${failed}"
