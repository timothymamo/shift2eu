#!/usr/bin/env bash
# The static rules of the OpenTofu configuration under an infra root (issue #422): what tofu test cannot
# state, read from the files themselves. dev/infra-check.sh runs it on infra/ before any tofu command;
# the fixtures in dev/testdata/infra-invalid (one broken rule each) run it on themselves. It reads files
# only: no tofu, no network, no credential.
#
# Each broken rule prints one line on stderr, "infra-rules: [<rule>] <file>[:<line>]: <why>", and the
# script exits 1; with every rule holding it prints nothing and exits 0; a usage error exits 2. The rules,
# by tag:
#   [no-config]      the root holds no configuration (*.tf, *.tofu)
#   [json-config]    a JSON configuration or test file (*.tf.json, *.tofu.json, *.tftest.json,
#                    *.tofutest.json), which these rules do not read
#   [symlink]        a symlink anywhere under the root: tofu would load what it points at, unread here
#   [override-file]  an override file (override.tf, *_override.tf, the .tofu and .json forms): it rewrites
#                    other files' blocks where a review of those files does not see it
#   [parse]          a heredoc, or a string, comment or bracket left open: the rules fail closed
#   [api-key]        a scaleway_iam_api_key resource or data source: the deploy key is made by hand, so no
#                    secret reaches the state
#   [bucket-acl]     a scaleway_object_bucket_acl resource
#   [acl]            an acl argument, even "private"
#   [visibility]     a visibility argument (an object's ACL), even "private"
#   [iam-policy-count], [iam-application-count]
#                    not exactly one scaleway_iam_policy, or scaleway_iam_application, under the root
#   [iam-repeat]     count or for_each on either of them
#   [iam-other]      any other scaleway_iam_* resource
#   [provider-type]  a resource, data source or provider block of any provider but scaleway
#   [provider-source] a required_providers entry whose source is not exactly "scaleway/scaleway"
#   [lock-file]      a .terraform.lock.hcl naming any provider but registry.opentofu.org/scaleway/scaleway
#   [module-source]  a module whose source is not a local ./ path
#   [backend-count]  a root configuration (a directory with *.tf or *.tofu) without exactly one backend
#   [backend-s3]     a backend other than s3 (never local)
#   [cloud-block]    a cloud block
#   [required-version] a root without exactly one required_version, or one other than infra/tofu.yaml's
#                    tofuVersion without its v
#   [test-mock]      a test file (*.tftest.hcl, *.tofutest.hcl) without an unaliased
#                    mock_provider "scaleway", so tofu test would run the real provider
#   [test-provider]  a test file with a provider block
# The HCL is read by dev/infra-rules.awk.
#
# Usage: dev/infra-rules.sh <infra root>   (from the repository root: dev/infra-rules.sh infra)
set -euo pipefail

if [ "$#" -ne 1 ] || [ ! -d "$1" ]; then
  echo >&2 "usage: dev/infra-rules.sh <infra root directory>"
  exit 2
fi
root="${1%/}"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
pins="${here}/../infra/tofu.yaml"

failed=0
fail() { # <rule> <where> <why>
  echo >&2 "infra-rules: [$1] $2: $3"
  failed=1
}

pinned="$(sed -n 's/^tofuVersion: "v\([0-9][0-9.]*\)"$/\1/p' "${pins}")"
if [ -z "${pinned}" ]; then
  echo >&2 "infra-rules: ${pins} pins no tofuVersion like v1.2.3"
  exit 2
fi

# Lists the files under the root matching the find tests given, outside .terraform directories.
files() {
  find "${root}" -name .terraform -prune -o "$@" -print | LC_ALL=C sort
}

while IFS= read -r f; do
  [ -n "${f}" ] && fail symlink "${f}" "a symlink: tofu would load what it points at, which these rules do not read"
done <<<"$(files -type l)"

while IFS= read -r f; do
  [ -n "${f}" ] && fail json-config "${f}" "JSON configuration or tests, which these rules do not read"
done <<<"$(files -type f \( -name '*.tf.json' -o -name '*.tofu.json' -o -name '*.tftest.json' -o -name '*.tofutest.json' \))"

while IFS= read -r f; do
  [ -n "${f}" ] && fail override-file "${f}" "an override file rewrites other files' blocks where a review of those files does not see it"
done <<<"$(files -type f \( -name 'override.tf' -o -name '*_override.tf' -o -name 'override.tofu' -o -name '*_override.tofu' -o -name 'override.tf.json' -o -name '*_override.tf.json' -o -name 'override.tofu.json' -o -name '*_override.tofu.json' \))"

while IFS= read -r f; do
  [ -n "${f}" ] || continue
  if grep -E '^[[:space:]]*provider[[:space:]]' "${f}" | grep -vqxE 'provider "registry\.opentofu\.org/scaleway/scaleway" \{'; then
    fail lock-file "${f}" "names a provider other than registry.opentofu.org/scaleway/scaleway"
  fi
done <<<"$(files -type f -name .terraform.lock.hcl)"

configs="$(files -type f \( -name '*.tf' -o -name '*.tofu' \))"
tests="$(files -type f \( -name '*.tftest.hcl' -o -name '*.tofutest.hcl' \))"
if [ -z "${configs}" ]; then
  fail no-config "${root}" "no OpenTofu configuration (*.tf, *.tofu)"
else
  args=()
  while IFS= read -r f; do
    [ -n "${f}" ] && args+=("${f}")
  done <<<"${configs}"$'\n'"${tests}"
  out="$(awk -v pinned="${pinned}" -v root="${root}" -f "${here}/infra-rules.awk" "${args[@]}")"
  if [ -n "${out}" ]; then
    while IFS= read -r line; do
      echo >&2 "infra-rules: ${line}"
    done <<<"${out}"
    failed=1
  fi
fi

exit "${failed}"
