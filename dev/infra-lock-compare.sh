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
# It reads only the lines tofu writes: blank lines, comments, provider "<address>" {, version = "...",
# constraints = "...", hashes = [, one quoted hash per line, ] and }, each in its place. Any other line,
# even one both files hold, fails as "<file>:<line>: a line ...", since the comparison could not see what
# it says; so does a hash or a provider block one file lists twice, which a set comparison would hide. A
# value outside a strict printable set (an address, version, constraint or hash holding ##[, which would
# forge a GitHub Actions annotation, or a control character) is never echoed: the line says "an
# unprintable value" in its place.
#
# Usage: dev/infra-lock-compare.sh <committed lock file> <regenerated lock file>
set -euo pipefail
export LC_ALL=C

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
# <version>", "constraints <address> <constraints>" and "hash <address> <hash>". Blank lines, comments and
# the hashes = [ and ] lines carry no fact. A line it cannot read prints "~unreadable <file>:<line> <why>";
# a hash listed twice in one block "~twice hash <address> <hash>"; a provider block listed twice
# "~twice provider <address>". A tab or control character in a value is kept as an unprintable mark, so
# the value is compared as itself but never printed.
facts() { # <lock file>
  awk '
    function value(s) { # the text between the line'"'"'s only two quotes
      sub(/^[^"]*"/, "", s)
      sub(/"[^"]*$/, "", s)
      gsub(/\t/, "<tab>", s)
      return s
    }
    function unreadable(why) {
      print "~unreadable\t" FILENAME ":" FNR "\t" why
    }
    /^[[:space:]]*$/ || /^[[:space:]]*(#|\/\/)/ { next }
    state == 0 && /^provider[[:space:]]+"[^"]*"[[:space:]]*\{[[:space:]]*$/ {
      addr = value($0)
      state = 1
      if (addr in seenProvider) {
        print "~twice\tprovider\t" addr
      } else {
        print "provider\t" addr
      }
      seenProvider[addr] = 1
      next
    }
    state == 1 && /^[[:space:]]*(version|constraints)[[:space:]]*=[[:space:]]*"[^"]*"[[:space:]]*$/ {
      key = $0
      sub(/^[[:space:]]*/, "", key)
      sub(/[[:space:]]*=.*$/, "", key)
      print key "\t" addr "\t" value($0)
      next
    }
    state == 1 && /^[[:space:]]*hashes[[:space:]]*=[[:space:]]*\[[[:space:]]*$/ { state = 2; next }
    state == 1 && /^}[[:space:]]*$/ { state = 0; next }
    state == 2 && /^[[:space:]]*"[^"]*"[[:space:]]*,?[[:space:]]*$/ {
      h = value($0)
      if ((addr, h) in seenHash) {
        print "~twice\thash\t" addr "\t" h
      }
      seenHash[addr, h] = 1
      print "hash\t" addr "\t" h
      next
    }
    state == 2 && /^[[:space:]]*\][[:space:]]*$/ { state = 1; next }
    {
      unreadable("a line that is none of the shapes tofu writes in a lock file, here")
    }
    END {
      if (state != 0) {
        unreadable("the file ends inside a provider block")
      }
    }
  ' "$1" | sort -u
}

# A value as it may be printed: itself when it is in the printable set of its kind, else "an unprintable
# value".
printable() { # <kind: address, version, constraints or hash> <value>
  local re
  case "$1" in
    address) re='^[A-Za-z0-9._/-]+$' ;;
    hash) re='^[a-z0-9]+:[A-Za-z0-9+/=]+$' ;;
    *) re='^[0-9A-Za-z.+<>=~!, -]*$' ;;
  esac
  if [[ "$2" =~ ${re} ]]; then
    printf '%s' "$2"
  else
    printf 'an unprintable value'
  fi
}

committed="$(facts "$1")"
regenerated="$(facts "$2")"

failed=0
fail() { # <where> <what>
  echo >&2 "infra-lock-compare: [lock-hashes] $1: $2"
  failed=1
}

# What a file's reading found wrong in itself: a line it could not read, or a fact listed twice.
problems() { # <committed|regenerated> <its facts>
  local kind a b c
  while IFS=$'\t' read -r kind a b c; do
    case "${kind}" in
      "~unreadable") fail "${a}" "${b}" ;;
      "~twice")
        if [ "${a}" = hash ]; then
          fail "$(printable address "${b}")" "hash $(printable hash "${c}") listed twice in the $1 file"
        else
          fail "$(printable address "${b}")" "a provider block listed twice in the $1 file"
        fi
        ;;
    esac
  done <<<"$(grep '^~' <<<"$2" || true)"
}
problems committed "${committed}"
problems regenerated "${regenerated}"
committed="$(grep -v '^~' <<<"${committed}" || true)"
regenerated="$(grep -v '^~' <<<"${regenerated}" || true)"

# <committed|regenerated> holds a fact the other file does not.
report() { # <which file holds it> <the other file> <fact line>
  local kind addr rest what
  IFS=$'\t' read -r kind addr rest <<<"$3"
  case "${kind}" in
    provider) what="a provider block only the $1 file holds" ;;
    version) what="version $(printable version "${rest}") only in the $1 file" ;;
    constraints) what="constraints \"$(printable constraints "${rest}")\" only in the $1 file" ;;
    hash) what="hash $(printable hash "${rest}") only in the $1 file (missing from the $2 one)" ;;
  esac
  fail "$(printable address "${addr}")" "${what}"
}

while IFS= read -r line; do
  [ -n "${line}" ] && report committed regenerated "${line}"
done <<<"$(comm -23 <(printf '%s\n' "${committed}") <(printf '%s\n' "${regenerated}"))"
while IFS= read -r line; do
  [ -n "${line}" ] && report regenerated committed "${line}"
done <<<"$(comm -13 <(printf '%s\n' "${committed}") <(printf '%s\n' "${regenerated}"))"

exit "${failed}"
