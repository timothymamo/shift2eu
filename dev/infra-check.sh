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
# test file mocking the provider, ...). Then tofu fmt -check, init, validate and test.
#
# Even on the admin machine, with the maintainer's own keys exported, tofu cannot reach Scaleway from
# here: it runs under env -i with an allow list, so no SCW_*, AWS_* or TF_* variable of the caller's
# reaches it (TF_CLI_ARGS*, TF_VAR_*, TF_CLI_CONFIG_FILE included); the Scaleway and AWS configuration
# files are /dev/null; HOME is an empty directory under bin/tofu, so no ~/.config/scw, ~/.aws or
# ~/.terraform.d is read. The provider cache and the working directory go under bin/tofu (git-ignored), so
# the checks never touch a .terraform directory the maintainer's real init left in infra/site.
#
# Usage: dev/infra-check.sh   (from the repository root, where make runs it)
set -euo pipefail

root="$(pwd)"
site=infra/site

dev/infra-rules.sh infra

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
  "TF_DATA_DIR=${root}/bin/tofu/data/site"
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

tofu() {
  env -i "${clean[@]}" "${root}/dev/tofu.sh" "$@"
}

tofu fmt -check -diff -recursive infra
tofu -chdir="${site}" init -backend=false -input=false -lockfile=readonly
tofu -chdir="${site}" validate
tofu -chdir="${site}" test
