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
  default; `region` defaults to `nl-ams`, `bucket_name` to `shift2eu-site-www`, `public_read` to `false`
  (set it only if the runbook's R5 finds that Edge Services cannot serve private objects).
- `.terraform.lock.hcl`: the provider's hashes for darwin_arm64, linux_amd64 and linux_arm64. After a
  provider change, refresh it with
  `dev/tofu.sh -chdir=infra/site providers lock -platform=darwin_arm64 -platform=linux_amd64 -platform=linux_arm64`.
- `tests/`: `tofu test` files, with the Scaleway provider mocked.

`make infra-check` (part of `make check`, and CI's Test job) runs `dev/infra-check.sh`: no credential, no
call to Scaleway, no state passphrase (the state is not encrypted, ADR 0039). Besides `fmt`, `validate`
and `test`, it runs the static rules of `dev/infra-rules.sh` (which lists them): among them no API key
resource, no bucket ACL or `visibility`, exactly one IAM application and policy, only `scaleway/scaleway`,
local modules only, none in a directory starting with `.` and none naming an applied root, one `s3`
backend for every applied root, no configuration file directly in `infra/`, no provider `api_url` and no
provider setting but `organization_id` and `region` (`[provider-setting]`), top-level blocks of the
allowed types only (`[block-type]`: no `moved`, `removed`, `check` or `override_resource`), a lock file of
only the lines `tofu init` writes for `scaleway/scaleway` (`[lock-file]`), no
override, JSON or hidden (`.`-named, which OpenTofu ignores) files, every test file mocking the
provider, no `provisioner` or `connection` block, no `*.auto.tfvars` (`.json` too), no setting in the
`s3` backend block beyond `key`, `use_lockfile` and the four `skip_*` ones, no `scaleway_object` (the
release deploys the objects), no call, in a configuration or test file, of a function that reads the
local machine (`file`, `templatefile`, `templatestring`, `fileset`, `pathexpand`, `abspath`, ...), nothing
in a `terraform` block but `required_version`, `required_providers`, `backend` and `cloud`
(`[terraform-setting]`: no `encryption`, whose `external` key provider runs a local command), no `import`
block, no `mock_provider` with a `source` and no `*.tfmock.hcl` or `*.tfmock.json` file, and only the
resource types `main.tf` uses today, no data or ephemeral type (`[resource-type]`). A new resource type
extends that allow list in `dev/infra-rules.awk` in the same PR as the resource.
`init`, `validate` and `test` run in every applied root (each directory directly under `infra/`).

`tofu test` loads a `terraform.tfvars` in `infra/site` (and in `tests/`) without being asked: on the admin
machine, where the maintainer's copy lives, the offline tests run with the maintainer's values, while CI,
which has none, runs them with the defaults. tofu loads `terraform.tfvars`, then `terraform.tfvars.json`,
then every `*.auto.*`: keep exactly one of `terraform.tfvars` or `terraform.tfvars.json` on the admin
machine (git tracks neither). A `*.auto.tfvars`, which tofu would load too, is refused (`[tfvars-file]`),
so no committed file can override those values.
It runs tofu with none of the caller's `SCW_*`, `AWS_*` or `TF_*` variables and no Scaleway or AWS
configuration file, so it cannot reach Scaleway even where the maintainer's keys are exported. Only the
maintainer runs `init` with the backend, `plan` and `apply`, on the admin machine, always through
`dev/tofu.sh`, which re-checks the pinned release's SHA-256 and extracts it afresh on every run. Agents
never do.
