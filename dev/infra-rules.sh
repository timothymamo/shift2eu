#!/usr/bin/env bash
# The static rules of the OpenTofu configuration under an infra root (issue #422): what tofu test cannot
# state, read from the files themselves. dev/infra-check.sh runs it on infra/ before any tofu command;
# TestInfraRules (test/devenv/infrarules_test.go) runs it on copies of infra/ with one broken rule each.
# It reads files only: no tofu, no network, no credential. It runs and is tested in the VM and in CI
# (bash and gawk); the BSD tools on macOS are untested.
#
# Each broken rule prints one line on stderr, "infra-rules: [<rule>] <file>[:<line>]: <why>", and the
# script exits 1; with every rule holding it prints nothing and exits 0; a usage error exits 2. The rules,
# by tag:
#   [no-config]      the root holds no configuration (*.tf, *.tofu)
#   [hidden-file]    a configuration or test file (any form above or below, JSON too) whose name starts with
#                    ".": OpenTofu ignores it, so it counts toward no other rule (.terraform.lock.hcl is
#                    no such file)
#   [json-config]    a JSON configuration or test file (*.tf.json, *.tofu.json, *.tftest.json,
#                    *.tofutest.json), which these rules do not read
#   [tfvars-file]    a *.auto.tfvars or *.auto.tfvars.json anywhere under the root, hidden ones too: tofu
#                    loads terraform.tfvars, then terraform.tfvars.json, then every *.auto.* unasked, so it
#                    would override the maintainer's values (a plain terraform.tfvars or
#                    terraform.tfvars.json, the maintainer's own on the admin machine, exactly one of them,
#                    is not refused; git tracks neither)
#   [mock-file]      a *.tfmock.hcl or *.tfmock.json anywhere under the root, hidden ones too: mocks belong
#                    in the test file that uses them
#   [symlink]        a symlink anywhere under the root: tofu would load what it points at, unread here
#   [override-file]  an override file (override.tf, *_override.tf, the .tofu and .json forms): it rewrites
#                    other files' blocks where a review of those files does not see it
#   [shadowed-file]  a <name>.tofu beside <name>.tf, or <name>.tofutest.hcl beside <name>.tftest.hcl:
#                    OpenTofu loads only the .tofu one, so the reviewed twin is not what runs
#   [parse]          a heredoc; a string, comment or bracket left open; a backslash in a block label or
#                    quoted key (HCL decodes escapes such as _), or a $${ or %%{ in a block label (HCL
#                    reads them as ${ and %{); a block header whose labels are not
#                    plain identifiers or literal strings. The rules fail closed: after a [parse] line
#                    no other rule is reported
#   [api-key]        a scaleway_iam_api_key resource or data source: the deploy key is made by hand, so no
#                    secret reaches the state
#   [provisioner]    a provisioner or connection block at any depth (in a resource, a removed block or a
#                    test file): it runs commands on the machine that applies or tests, where the keys live
#   [object]         a scaleway_object resource: tofu never uploads objects, the release deploys them
#   [file-function]  a call of file, filebase64, filebase64sha256, filebase64sha512, fileexists, fileset,
#                    templatefile, templatestring, filemd5, filesha1, filesha256, filesha512, pathexpand or
#                    abspath in a configuration or test file, a template's ${...} too: the plan would read
#                    the machine that runs it. A name in a comment or in a string without a template is no
#                    call
#   [import-block]   a top-level import block in a configuration file: every resource is created fresh,
#                    never adopted (a test file's import block gets [block-type] alone)
#   [resource-type]  a scaleway_* resource type infra/site/main.tf does not use, or any scaleway_* data or
#                    ephemeral type, that no rule above or below names: an allow list in
#                    dev/infra-rules.awk, extended in the same PR as the new resource. A type a specific
#                    rule names ([api-key], [object], [bucket-acl], [iam-other], [provider-type]) gets
#                    only that rule's line
#   [bucket-acl]     a scaleway_object_bucket_acl resource
#   [acl]            an acl argument, even "private"
#   [visibility]     a visibility argument (an object's ACL), even "private"
#   [iam-policy-count], [iam-application-count]
#                    not exactly one scaleway_iam_policy, or scaleway_iam_application, under the root
#   [iam-repeat]     count or for_each on either of them
#   [iam-other]      any other scaleway_iam_* resource
#   [provider-type]  a resource, data source or provider block of any provider but scaleway
#   [provider-source] a required_providers entry whose source is not exactly "scaleway/scaleway"
#   [lock-file]      a .terraform.lock.hcl with a line of a shape tofu init does not write: only blank and
#                    # comment lines, the provider "registry.opentofu.org/scaleway/scaleway" { header (any
#                    whitespace between its tokens), version = "...", constraints = "...", hashes = [, a
#                    quoted hash with an optional trailing comma, ] and } pass, so a second provider, or one
#                    behind a /* */ comment, is refused. The first such line is named, one line per file
#   [provider-alias] alias in a configuration's provider block: a second provider configuration
#   [provider-credentials] access_key, secret_key or profile in a configuration's provider block: the
#                    credentials come from the admin machine's environment, never from a file
#   [provider-endpoint] api_url in a configuration's provider block: the provider talks to Scaleway's own
#                    API, never to an endpoint a file names
#   [provider-setting] an attribute or nested block in a configuration's provider "scaleway" other than
#                    organization_id and region (a default project_id, ...), besides the ones a rule above
#                    names (alias, access_key, secret_key, profile, api_url), which get only that rule's line
#   [block-type]     a top-level block of a type outside the allow list: terraform, provider, resource, data,
#                    ephemeral, variable, locals, output, module and import in a configuration file; run,
#                    mock_provider, variables and provider in a test file. moved, removed, check, list,
#                    override_resource, a mock_provider in a configuration, ... are blocks no rule reads. A
#                    type a rule names ([test-provider]) gets only that rule's line; the
#                    blocks nested in a refused one get no [block-type] line (a provisioner there keeps its own)
#   [module-source]  a module, in the configuration or in a test's run block, whose source is not a local
#                    ./ path, or has a .. segment or another segment starting with "." (such as
#                    ./.terraform/m, a directory these rules prune); or a configuration's module whose
#                    source resolves (after ./, . segments and a trailing /) to an applied root
#   [root-file]      a configuration or test file directly in the root's own directory: configurations
#                    live in the applied roots beneath it
#   [backend-count]  a configuration directory without exactly one backend. Every applied root (a
#                    directory directly under the root, today infra/site) is judged; a deeper directory
#                    is exempt only when a configuration's local module names it (a test file's run-block
#                    module exempts nothing)
#   [backend-s3]     a backend other than s3 (never local)
#   [backend-setting] an attribute or nested block in backend "s3" other than key, use_lockfile and the four
#                    skip_* settings (insecure, endpoints, assume_role, ...): the bucket, region and
#                    endpoint come from the git-ignored backend.hcl
#   [cloud-block]    a cloud block
#   [terraform-setting] an attribute or block in a top-level terraform block other than required_version,
#                    required_providers, backend and cloud (encryption, provider_meta, experiments, ...):
#                    an encryption key provider "external" runs a local command at init, plan and apply,
#                    and its kms and openbao providers send ambient credentials to an endpoint it names
#   [required-version] a configuration directory judged as [backend-count] judges it without exactly one
#                    required_version, or one other than infra/tofu.yaml's
#                    tofuVersion without its v
#   [test-mock]      a test file (*.tftest.hcl, *.tofutest.hcl) without an unaliased
#                    mock_provider "scaleway", so tofu test would run the real provider
#   [test-provider]  a test file with a provider block
#   [test-providers] providers = ... in a test's run block, which hands the run another provider
#                    configuration than the mock
#   [mock-source]    a source attribute in a test file's mock_provider: its mocks would come from outside
#                    the test file
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

# The names OpenTofu reads as configuration or tests. A hidden one (basename starting with ".") is one
# OpenTofu ignores (configs.IsIgnoredFile): [hidden-file] refuses it, and no other rule reads it.
read_names=(\( -name '*.tf' -o -name '*.tofu' -o -name '*.tf.json' -o -name '*.tofu.json'
  -o -name '*.tftest.hcl' -o -name '*.tofutest.hcl' -o -name '*.tftest.json' -o -name '*.tofutest.json' \))

while IFS= read -r f; do
  [ -n "${f}" ] && fail hidden-file "${f}" "a hidden file, which OpenTofu ignores: what it holds (a backend, a pin, a mock) is not what runs, so give it a name without the leading ."
done <<<"$(files -type f -name '.*' "${read_names[@]}")"

while IFS= read -r f; do
  [ -n "${f}" ] && fail tfvars-file "${f}" "an auto-loaded variables file: tofu reads it after terraform.tfvars without anyone naming it, so a value git holds would override the maintainer's own"
done <<<"$(files -type f \( -name '*.auto.tfvars' -o -name '*.auto.tfvars.json' \))"

while IFS= read -r f; do
  [ -n "${f}" ] && fail mock-file "${f}" "a mock data file: the mocks belong in the test file that uses them, never in a file beside it that a mock_provider's source could load"
done <<<"$(files -type f \( -name '*.tfmock.hcl' -o -name '*.tfmock.json' \))"

while IFS= read -r f; do
  [ -n "${f}" ] && fail symlink "${f}" "a symlink: tofu would load what it points at, which these rules do not read"
done <<<"$(files -type l)"

while IFS= read -r f; do
  [ -n "${f}" ] && fail json-config "${f}" "JSON configuration or tests, which these rules do not read"
done <<<"$(files -type f ! -name '.*' \( -name '*.tf.json' -o -name '*.tofu.json' -o -name '*.tftest.json' -o -name '*.tofutest.json' \))"

while IFS= read -r f; do
  [ -n "${f}" ] && fail override-file "${f}" "an override file rewrites other files' blocks where a review of those files does not see it"
done <<<"$(files -type f ! -name '.*' \( -name 'override.tf' -o -name '*_override.tf' -o -name 'override.tofu' -o -name '*_override.tofu' -o -name 'override.tf.json' -o -name '*_override.tf.json' -o -name 'override.tofu.json' -o -name '*_override.tofu.json' \))"

while IFS= read -r f; do
  [ -n "${f}" ] || continue
  case "${f}" in
    *.tofutest.hcl) twin="${f%.tofutest.hcl}.tftest.hcl" ;;
    *) twin="${f%.tofu}.tf" ;;
  esac
  if [ -e "${twin}" ]; then
    fail shadowed-file "${f}" "beside ${twin}: OpenTofu loads only the .tofu file of the pair, so the reviewed ${twin##*/} is not what runs"
  fi
done <<<"$(files -type f ! -name '.*' \( -name '*.tofu' -o -name '*.tofutest.hcl' \))"

# The line shapes tofu init writes in a lock file for the one Scaleway provider, any whitespace between
# the tokens: anything else (a second provider, a header behind a /* comment, a line init never writes)
# is a line these rules did not read, so the first such line is refused.
lock_lines=(
  -e '^[[:space:]]*$'
  -e '^[[:space:]]*#.*$'
  -e '^[[:space:]]*provider[[:space:]]*"registry\.opentofu\.org/scaleway/scaleway"[[:space:]]*\{[[:space:]]*$'
  -e '^[[:space:]]*(version|constraints)[[:space:]]*=[[:space:]]*"[^"\\$%]*"[[:space:]]*$'
  -e '^[[:space:]]*hashes[[:space:]]*=[[:space:]]*\[[[:space:]]*$'
  -e '^[[:space:]]*"[a-z0-9]+:[A-Za-z0-9+/=]+"[[:space:]]*,?[[:space:]]*$'
  -e '^[[:space:]]*[]}][[:space:]]*$'
)
while IFS= read -r f; do
  [ -n "${f}" ] || continue
  odd="$(LC_ALL=C grep -nvE "${lock_lines[@]}" "${f}" | head -n 1 || true)"
  if [ -n "${odd}" ]; then
    fail lock-file "${f}:${odd%%:*}" "an unexpected line, '${odd#*:}': a lock file holds only what tofu init writes for registry.opentofu.org/scaleway/scaleway (its provider header, version, constraints, hashes, blank and # comment lines), so another provider or a line init never writes is refused"
  fi
done <<<"$(files -type f -name .terraform.lock.hcl)"

configs="$(files -type f ! -name '.*' \( -name '*.tf' -o -name '*.tofu' \))"
tests="$(files -type f ! -name '.*' \( -name '*.tftest.hcl' -o -name '*.tofutest.hcl' \))"
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
