#!/usr/bin/env bash
# The offline checks of the OpenTofu configuration in infra/, for make infra-check (which make check runs)
# and CI (issue #422). They need no credential and contact no cloud: init -backend=false reads no state,
# and the tests in infra/site/tests mock the Scaleway provider. The only downloads are the pinned OpenTofu
# release (dev/tofu.sh, checked against its SHA-256 pin) and the pinned provider from the OpenTofu
# registry, checked against .terraform.lock.hcl, which init may not change (-lockfile=readonly).
#
# First the rules tofu test cannot state, read from the files themselves:
#   - no scaleway_iam_api_key resource or data source anywhere in infra/: the deploy key is made by hand,
#     so no secret reaches the state;
#   - no scaleway_object_bucket_acl resource and no acl argument, not even "private": the bucket policy
#     alone decides who reads the site (ADR 0039), so no ACL can make an object or the bucket public;
#   - every root configuration has exactly one backend, and it is s3: never local, never a cloud block;
#   - no JSON configuration (*.tf.json, *.tofu.json), which the rules above would not read;
#   - infra/site's required_version is infra/tofu.yaml's tofuVersion, the release dev/tofu.sh runs.
# Then tofu fmt -check, init, validate and test.
#
# The provider cache and the working directory go under bin/tofu (git-ignored), so the checks never touch
# a .terraform directory the maintainer's real init left in infra/site. TF_CLI_ARGS* and a CLI
# configuration of the caller's are ignored, so they cannot change what is checked or where providers come
# from.
#
# Usage: dev/infra-check.sh   (from the repository root, where make runs it)
set -euo pipefail

root="$(pwd)"
tofu="${root}/dev/tofu.sh"
site=infra/site

for var in $(compgen -e); do
  case "${var}" in
    TF_CLI_ARGS | TF_CLI_ARGS_* | TF_CLI_CONFIG_FILE | TF_WORKSPACE | TF_DATA_DIR | TF_PLUGIN_CACHE_DIR) unset "${var}" ;;
  esac
done
export TF_CLI_CONFIG_FILE=/dev/null
export TF_DATA_DIR="${root}/bin/tofu/data/site"
export TF_PLUGIN_CACHE_DIR="${root}/bin/tofu/plugin-cache"
export TF_IN_AUTOMATION=1 TF_INPUT=0 CHECKPOINT_DISABLE=1
mkdir -p "${TF_PLUGIN_CACHE_DIR}"

failed=0
fail() { # <message>
  echo >&2 "infra-check: $1"
  failed=1
}

# The configuration files under infra/, without the working directories init writes.
configs="$(find infra -name .terraform -prune -o -type f \( -name '*.tf' -o -name '*.tofu' \) -print | LC_ALL=C sort)"
if [ -z "${configs}" ]; then
  fail "no OpenTofu configuration found under infra/"
fi

json="$(find infra -name .terraform -prune -o -type f \( -name '*.tf.json' -o -name '*.tofu.json' \) -print)"
if [ -n "${json}" ]; then
  fail "JSON configuration is not allowed under infra/, as these checks do not read it: ${json//$'\n'/ }"
fi

while IFS= read -r file; do
  [ -n "${file}" ] || continue
  if grep -nE '(resource|data)[[:space:]]+"?scaleway_iam_api_key"?' "${file}" >&2; then
    fail "${file} declares a scaleway_iam_api_key: the deploy API key is created by hand, so no secret reaches the state"
  fi
  if grep -nE 'resource[[:space:]]+"?scaleway_object_bucket_acl"?' "${file}" >&2; then
    fail "${file} declares a scaleway_object_bucket_acl: the bucket policy alone decides who reads the site (ADR 0039), and an ACL could make every object, or the bucket, public"
  fi
  if grep -nE '^[[:space:]]*acl[[:space:]]*=' "${file}" >&2; then
    fail "${file} sets an acl: the bucket policy alone decides who reads the site (ADR 0039)"
  fi
done <<<"${configs}"

# Every directory under infra/ that holds configuration is a root configuration with one s3 backend.
dirs="$(while IFS= read -r file; do [ -n "${file}" ] && dirname "${file}"; done <<<"${configs}" | LC_ALL=C sort -u)"
while IFS= read -r dir; do
  [ -n "${dir}" ] || continue
  backends="$(cat "${dir}"/*.tf "${dir}"/*.tofu 2>/dev/null | grep -E '^[[:space:]]*backend[[:space:]]' || true)"
  count="$(printf '%s' "${backends}" | grep -c . || true)"
  if [ "${count}" -ne 1 ]; then
    fail "${dir} declares ${count} backends, want exactly one: backend \"s3\""
  elif ! printf '%s\n' "${backends}" | grep -qE '^[[:space:]]*backend[[:space:]]+"s3"[[:space:]]*\{'; then
    fail "${dir}'s backend is not s3 (${backends#"${backends%%[![:space:]]*}"}): the state lives in a Scaleway bucket, never local"
  fi
  if cat "${dir}"/*.tf "${dir}"/*.tofu 2>/dev/null | grep -nE '^[[:space:]]*cloud[[:space:]]*\{' >&2; then
    fail "${dir} has a cloud block: the state lives in a Scaleway bucket"
  fi
done <<<"${dirs}"

want="$(sed -n 's/^tofuVersion: "v\([^"]*\)"$/\1/p' infra/tofu.yaml)"
got="$(sed -n 's/^[[:space:]]*required_version[[:space:]]*=[[:space:]]*"\([^"]*\)"[[:space:]]*$/\1/p' "${site}"/*.tf)"
if [ -z "${want}" ] || [ "${got}" != "${want}" ]; then
  fail "${site}'s required_version is \"${got}\", infra/tofu.yaml's tofuVersion v${want}: they must name one release"
fi

if [ "${failed}" -ne 0 ]; then
  exit 1
fi

"${tofu}" fmt -check -diff -recursive infra
"${tofu}" -chdir="${site}" init -backend=false -input=false -lockfile=readonly
"${tofu}" -chdir="${site}" validate
"${tofu}" -chdir="${site}" test
