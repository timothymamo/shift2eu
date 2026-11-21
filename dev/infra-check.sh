#!/usr/bin/env bash
# The offline checks of the OpenTofu configuration in infra/, for make infra-check (which make check runs)
# and CI (issue #422). They need no credential and contact no cloud: init -backend=false reads no state,
# and every test file mocks the Scaleway provider. The only downloads are the pinned OpenTofu release
# (dev/tofu.sh, checked against its SHA-256 pin) and the pinned provider from the OpenTofu registry,
# checked against .terraform.lock.hcl, which init may not change (-lockfile=readonly).
#
# First the static rules tofu test cannot state (dev/infra-rules.sh infra, which lists them: no API key
# resource, no ACL or visibility, exactly one IAM application and policy, only scaleway/scaleway, local
# modules only, one s3 backend per root, required_version equal to infra/tofu.yaml's tofuVersion, every
# test file mocking the provider, ...). Then tofu fmt -check, and init, validate and test in every applied
# root (each directory directly under infra/).
#
# tofu test reads a terraform.tfvars in the root (and in its tests/) unasked: on the admin machine the
# offline tests run with the maintainer's own values, in CI with the defaults. No *.auto.tfvars can join
# them: dev/infra-rules.sh refuses one ([tfvars-file]).
#
# Even on the admin machine, with the maintainer's own keys exported, tofu cannot reach Scaleway from
# here: it runs under env -i with an allow list, so no SCW_*, AWS_* or TF_* variable of the caller's
# reaches it (TF_CLI_ARGS*, TF_VAR_*, TF_CLI_CONFIG_FILE included); the Scaleway and AWS configuration
# files are /dev/null; HOME is an empty directory under bin/tofu, so no ~/.config/scw, ~/.aws or
# ~/.terraform.d is read. The provider cache and the working directory go under bin/tofu (git-ignored), so
# the checks never touch a .terraform directory the maintainer's real init left in an applied root.
#
# Usage: dev/infra-check.sh   (from the repository root, where make runs it)
set -euo pipefail

root="$(pwd)"

dev/infra-rules.sh infra

# Every applied root (each directory directly under infra/, today infra/site): the static rules judge
# each one, so init, validate and test run in each one too, never in infra/site alone.
applied=()
for dir in infra/*/; do
  applied+=("${dir%/}")
done
if [ "${#applied[@]}" -eq 0 ]; then
  echo >&2 "infra-check: no applied root under infra/"
  exit 1
fi

home="${root}/bin/tofu/home"
cache="${root}/bin/tofu/plugin-cache"
mkdir -p "${home}" "${cache}"

# The whole environment tofu (and dev/tofu.sh's download) sees. PATH finds curl, tar and the hash tool;
# TMPDIR, the proxy settings and the CA bundle location are passed on when set, as a download may need
# them, and none of them is a credential or changes what is checked.
clean=(
  "PATH=${PATH}"
  "HOME=${home}"
  "TF_CLI_CONFIG_FILE=/dev/null"
  "TF_PLUGIN_CACHE_DIR=${cache}"
  "TF_IN_AUTOMATION=1"
  "TF_INPUT=0"
  "CHECKPOINT_DISABLE=1"
  "SCW_CONFIG_PATH=/dev/null"
  "AWS_CONFIG_FILE=/dev/null"
  "AWS_SHARED_CREDENTIALS_FILE=/dev/null"
)
for var in TMPDIR HTTPS_PROXY https_proxy HTTP_PROXY http_proxy NO_PROXY no_proxy SSL_CERT_FILE SSL_CERT_DIR; do
  if [ -n "${!var:-}" ]; then
    clean+=("${var}=${!var}")
  fi
done

# TF_DATA_DIR is the applied root's own working directory under bin/tofu/data (bin/tofu/data/site for
# infra/site), so no two roots share an init.
data="${root}/bin/tofu/data/none"
tofu() {
  env -i "${clean[@]}" "TF_DATA_DIR=${data}" "${root}/dev/tofu.sh" "$@"
}

tofu fmt -check -diff -recursive infra
for dir in "${applied[@]}"; do
  data="${root}/bin/tofu/data/${dir##*/}"
  tofu -chdir="${dir}" init -backend=false -input=false -lockfile=readonly
  tofu -chdir="${dir}" validate
  tofu -chdir="${dir}" test
done
