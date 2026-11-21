# infra/site

The OpenTofu configuration of the site's Scaleway hosting (PRD #297 milestone 3, issue #422). What it
creates, what stays by hand and where the state lives are the ADR's and the PRD's *Runbook* section's to
say; this directory holds only the configuration and its offline tests.

- `versions.tf`: OpenTofu and the `scaleway/scaleway` provider, pinned exactly; the `s3` backend on
  Scaleway's endpoint, partially configured (`backend.hcl.example`).
- `variables.tf`: the IDs the maintainer gives (`terraform.tfvars.example`); none has a default.
- `.terraform.lock.hcl`: the provider's hashes for darwin_arm64, linux_amd64 and linux_arm64. After a
  provider change, refresh it with
  `dev/tofu.sh -chdir=infra/site providers lock -platform=darwin_arm64 -platform=linux_amd64 -platform=linux_arm64`.
- `tests/`: `tofu test` files, with the Scaleway provider mocked.

`make infra-check` (part of `make check`, and CI's Test job) runs `dev/infra-check.sh`: no credential, no
call to Scaleway. Only the maintainer runs `init` with the backend, `plan` and `apply`, on the admin
machine, always through `dev/tofu.sh`, which runs the release `infra/tofu.yaml` pins after checking its
SHA-256. Agents never do.
