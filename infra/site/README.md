# infra/site

The OpenTofu configuration of the site's Scaleway hosting (PRD #297 milestone 3, issue #422,
[ADR 0039](../../docs/adr/0039-create-the-sites-scaleway-hosting-with-opentofu.md)): the project that holds
only the site's bucket, the bucket (versioned, website mode, its bucket policy), the deploy IAM
application and its policy, and the Edge Services Starter plan and pipeline serving `www.shift2eu.com`.
The deploy API key, the state bucket, the Namecheap records and the GitHub `site` environment are made by
hand, in the PRD's *Runbook* section (R0 to R13), which the maintainer follows.

- `versions.tf`: OpenTofu and the `scaleway/scaleway` provider, pinned exactly; the `s3` backend on
  Scaleway's endpoint with state locking (`use_lockfile`), partially configured (`backend.hcl.example`).
- `providers.tf`: the provider, with the organization and region and no default project.
- `main.tf`: every resource; `outputs.tf`: the IDs and endpoints the runbook's hand steps need.
- `variables.tf`: the inputs (`terraform.tfvars.example`): `organization_id` and `admin_principal` have no
  default; `region` defaults to `nl-ams`, `bucket_name` to `shift2eu-site-www`.
- `.terraform.lock.hcl`: the provider's hashes for darwin_arm64, linux_amd64 and linux_arm64. After a
  provider change, refresh it with
  `dev/tofu.sh -chdir=infra/site providers lock -platform=darwin_arm64 -platform=linux_amd64 -platform=linux_arm64`.
- `tests/`: `tofu test` files, with the Scaleway provider mocked.

`make infra-check` (part of `make check`, and CI's Test job) runs `dev/infra-check.sh`: no credential, no
call to Scaleway, no state passphrase (the state is not encrypted, ADR 0039). Besides `fmt`, `validate`
and `test`, it refuses an API key resource, any bucket ACL, a backend other than `s3` and JSON
configuration. Only the maintainer runs `init` with the backend, `plan` and `apply`, on the admin
machine, always through `dev/tofu.sh`, which runs the release `infra/tofu.yaml` pins after checking its
SHA-256. Agents never do.
