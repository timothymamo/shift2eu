# 0039. Create the site's Scaleway hosting with OpenTofu, its state in a locked bucket outside the site's project

- Status: Proposed
- Date: 2026-11-21

Part of [PRD #297](../../prds/297-web-assessment-site.md) milestone 3, issue #422. It records how the
hosting is created; the deploy job, the attestation and the live check get milestone 3's hosting ADR
([ADR 0042](0042-host-the-site-behind-edge-services-and-deploy-it-from-the-release-with-a-bucket-scoped-key.md),
amended in milestone 4), which this one does not replace.

## Context

The PRD's *Runbook* had the maintainer create the site's Scaleway hosting by hand in the console: a
project, a bucket with versioning, its website setting and its bucket policy, an IAM application and
policy, and an Edge Services pipeline for `www.shift2eu.com`. A hand-made bucket policy or IAM rule is the
part the site's security rests on (Hosting decisions 5 and 11), and nothing would hold it after the day
it was typed. The maintainer decided on 2026-11-21 (issue #422) to create it with OpenTofu, from
`infra/site/`, with the state in a Scaleway bucket, never in git, and to keep three things by hand: the
deploy API key, the Namecheap DNS records and the GitHub `site` environment.

Agents never hold Scaleway credentials (CLAUDE.md rule 3), so only the maintainer runs `init` with the
backend, `plan` and `apply`, on the admin machine (mac-mini-home, `docs/dev-environment.md`), and the
agents' checks must run offline.

What the pinned provider can do was read from its schema (`dev/tofu.sh providers schema -json` after an
offline init) and its documentation at
[v2.93.0](https://github.com/scaleway/terraform-provider-scaleway/tree/v2.93.0/docs/resources); what
Scaleway can do from its documentation's source,
[scaleway/docs-content](https://github.com/scaleway/docs-content) at commit `1ec45ae`.

## Decision

**OpenTofu 1.13.9 and `scaleway/scaleway` 2.93.0, pinned exactly**, run only through `dev/tofu.sh`. It
downloads the release tarball over HTTPS only (`curl --proto '=https' --tlsv1.2`), keeps it only if its
SHA-256 is the platform's pin in `infra/tofu.yaml`, and on every run hashes the kept tarball again and
extracts `tofu` into a fresh temporary directory, so no binary left in `bin/` is ever trusted. Only the
tarball's digest is pinned: Renovate keeps it current (`github-release-attachments`), and it could not
keep a digest of the extracted binary. The "Infra tools" workflow (`.github/workflows/infra-tools.yml`,
on every pull request and weekly) checks every pin against the release's `tofu_<version>_SHA256SUMS`,
whose cosign signature must come from `opentofu/opentofu`'s `release.yml` on its `v<major>.<minor>`
branch. Its `verify-provider-lock` job (issue #434, on the same triggers) runs `tofu providers lock` in a
scratch copy of the `infra/site` root without its lock file or `.terraform`, and
`dev/infra-lock-compare.sh` fails unless each provider's version, constraints and set of hashes equal
the committed `.terraform.lock.hcl`'s: a lock file that lost a platform's hash, or holds an extra one,
fails there rather than at the next machine's init. The regenerated file holds every hash the registry
publishes for the provider version, whatever the `-platform` list (an `h1` hash for each platform
the provider ships, 13 for scaleway/scaleway 2.93.0, and the `zh` set from its signed `SHA256SUMS`); the `-platform` flags for
darwin_arm64, linux_amd64 and linux_arm64 make tofu download and verify those packages. A weekly failure
means the registry's published set changed, and the lock file is refreshed with `infra/site/README.md`'s
command. The comparison reads only the lines tofu writes, refuses a hash listed twice, and never echoes a
value outside a strict printable set. It needs no credential and never reaches Scaleway.
Every download `dev/tofu.sh` and the workflow make carries curl's `--connect-timeout` and `--max-time`.

`make infra-check` (in `make check` and CI) runs the static rules (`dev/infra-rules.sh infra`, which
lists them), `fmt -check`, `init -backend=false`, `validate` and `tofu test` against `mock_provider`: no
credential, no state, no call to Scaleway, no passphrase. It runs tofu under `env -i` with an allow list:
no `SCW_*`, `AWS_*` or `TF_*` variable of the caller's, `SCW_CONFIG_PATH`, `AWS_CONFIG_FILE` and
`AWS_SHARED_CREDENTIALS_FILE` set to `/dev/null`, and `HOME` an empty directory, so no credential of the
caller's reaches it, even on the admin machine with the maintainer's keys exported. `init`, `validate`
and `test` run in every applied root. `tofu test` reads a local `terraform.tfvars` in the root (and its
`tests/`) unasked, so on the admin machine the offline tests run with the maintainer's values and in CI
with the defaults. That holds only
while the configuration itself names no credential and no real provider for a test, which the static
rules keep: a configuration's `provider` block has no `alias` (a second configuration a test could pick)
and no `access_key`, `secret_key`, `profile` or `api_url`, and sets nothing but `organization_id` and
`region` (`[provider-setting]`: no default `project_id`, no nested block); a test file declares an unaliased
`mock_provider "scaleway"`, no `provider` block, and no `providers` in a `run` block. Among the other
static rules: `required_providers` names only `scaleway/scaleway`, and the lock file holds only the line
shapes `tofu init` writes for that provider (`[lock-file]`: its header, `version`, `constraints`, `hashes`,
blank and `#` comment lines, so neither a second provider nor one behind a `/* */` comment passes); a
configuration file holds only `terraform`, `provider`, `resource`, `data`, `ephemeral`, `variable`,
`locals`, `output`, `module` and `import` blocks at its top level, a test file only `run`,
`mock_provider`, `variables` and `provider` (`[block-type]`: a `moved`, `removed`, `check` or
`override_resource` block is one no rule reads), and directly in a test's `run` only `override_resource`,
`override_data`, `override_module`, `assert`, `variables`, `module`, `plan_options` and `expect_failures`,
in its `mock_provider` only `mock_resource`, `mock_data`, `override_resource` and `override_data`
(`[test-nested-block]`, issue #434), and a `run` or `mock_provider` label is an identifier
(`[test-label]`; every block label enters the rules' block path with `%`, `/` and `:` escaped, so no label
hides a block's children; the `%` escape is defence in depth, keeping the escaping injective, since no rule
today tells apart the paths two labels such as `p/q` and `p%2Fq` would otherwise share; a message that quotes a
label alone quotes it as the source writes it, and one that prints a block path keeps the escaped form, but
prints a label only if every character is in `A-Za-z0-9_-`, `/`, `%`, `:`, `.` and space, writing `an
unprintable label` in its place otherwise, and a block path, key or source string likewise, so a label
such as `##[error]x` never forges a GitHub Actions annotation in the CI log);
every resource, data source and provider is Scaleway's; a module's source, in the configuration or in a
test's `run` block, is a local `./` path without a `..` segment or another segment starting with `.`
(such as `./.terraform/m`, under the `.terraform` directory the rules skip); every applied root (a directory directly
under `infra/`, today `infra/site`) has exactly one `s3` backend and one `required_version`, which no
module source can waive: a configuration's module may not name an applied root, a test's `run`-block
module exempts no directory, and no configuration or test file sits directly in `infra/`; exactly one `scaleway_iam_application`
and one `scaleway_iam_policy`, without `count` or `for_each`, and no other IAM resource; no `acl` or
`visibility` argument; no override file, JSON file, symlink, or `.tofu` (`.tofutest.hcl`) file beside a
`.tf` (`.tftest.hcl`) file of the same name, which OpenTofu would load instead; no hidden configuration
or test file (a name starting with `.`), which OpenTofu ignores, so the rules refuse it and read it for
no other rule; no `provisioner` or `connection` block at any depth, in a configuration or test file (a
`local-exec` would run on the admin machine, where the Scaleway keys and the GitHub admin token live);
no `*.auto.tfvars` or `*.auto.tfvars.json`, hidden ones too, which tofu loads after `terraform.tfvars`
and `terraform.tfvars.json` unasked (the maintainer's own `terraform.tfvars` or `terraform.tfvars.json`,
exactly one of them, stays allowed and out of git); in the committed
`backend "s3"` block only `key`, `use_lockfile`, `skip_credentials_validation`, `skip_region_validation`,
`skip_requesting_account_id` and `skip_s3_checksum`, so no `insecure`, `endpoints`, `assume_role` or
other setting can redirect or weaken the state's transport; no `scaleway_object` resource (tofu never
uploads an object, the release does); no call, in a configuration or test file and in a template's
`${...}` too, of a function that reads the machine running tofu (`file`, `filebase64*`, `fileexists`,
`fileset`, `templatefile`, `templatestring`, `filemd5`, `filesha*`, `pathexpand`, `abspath`); nothing in
a top-level `terraform` block but `required_version`, `required_providers`, `backend` and `cloud`
(`[terraform-setting]`: an `encryption` block's `external` key provider runs a local command at init,
plan and apply, and its `kms` and `openbao` providers send ambient credentials to an endpoint it names;
`provider_meta` and `experiments` change how tofu runs); no `import` block (`[import-block]`: every
resource is created fresh, never adopted); no `mock_provider` with a `source` (`[mock-source]`) and no
`*.tfmock.hcl` or `*.tfmock.json` file (`[mock-file]`), so a test's mocks are what its file holds; only
the resource types `infra/site/main.tf` uses, and no data or ephemeral type (`[resource-type]`, an allow
list in `dev/infra-rules.awk`; a type a specific rule names, such as `scaleway_iam_api_key` or
`scaleway_object`, gets only that rule's line). A new resource type extends the list in the same PR as
the resource, so its review sees both; an empty file is judged as any other;
and a file the rules
cannot read (a heredoc, an unbalanced string or bracket, a backslash escape in a block label or quoted
key, a `$${` or `%%{` escape in a block label, a block header of anything but identifiers and literal strings) fails them, with no other verdict.

### What OpenTofu creates (`infra/site/main.tf`)

| Resource | Key arguments |
|---|---|
| `scaleway_account_project.site` | `shift2eu-site`: holds only the site's bucket and the pipeline serving it |
| `scaleway_object_bucket.site` | `project_id` the site's project, `region` `var.region` (default `nl-ams`, Hosting decision 9's prices), `versioning { enabled = true }`, no `acl`, no `force_destroy` |
| `scaleway_object_bucket_website_configuration.site` | `index_document { suffix = "index.html" }`, no error document (the site ships no error page) |
| `scaleway_object_bucket_policy.site` | version `2023-04-17`, Allow only, three statements, a fourth with `public_read` (below) |
| `scaleway_iam_application.deploy` | the release's deploy job |
| `scaleway_iam_policy.deploy` | one rule: `ObjectStorageObjectsWrite` and `ObjectStorageObjectsRead`, `project_ids` the site's project, no `organization_id`, no user or group |
| `scaleway_edge_services_plan.site` | `starter` (Hosting decision 9), in the site's project |
| `scaleway_edge_services_pipeline.site` | one pipeline |
| `scaleway_edge_services_backend_stage.site` | `s3_backend_config`: the site bucket, its region, `is_website = true` |
| `scaleway_edge_services_cache_stage.site` | after the backend; `fallback_ttl = 300` (only for a response without `Cache-Control`), `include_cookies = false` |
| `scaleway_edge_services_tls_stage.site` | after the cache; `managed_certificate = true` (Let's Encrypt, decision 10's CAA) |
| `scaleway_edge_services_dns_stage.site` | after TLS; `fqdns = ["www.shift2eu.com"]` |
| `scaleway_edge_services_head_stage.site` | the DNS stage is the pipeline's head |

No WAF and no route stage: one backend serves every path. Every child of the bucket and every Edge
Services resource names `project_id` itself, as the provider requires outside the default project; the
provider has no default project.

**The bucket policy** allows, and nothing else:

1. `AdminManagesTheBucket`: `s3:*` on the bucket and its objects to `var.admin_principal`, the
   maintainer's own `user_id:` or `application_id:` (never `*`, no default). A bucket policy limits even
   the bucket's owner ("You will lose access to your bucket if you are not the owner of the Organization,
   and if you are not explicitly allowed by the bucket policy",
   [bucket policies](https://www.scaleway.com/en/docs/object-storage/api-cli/bucket-policy/)), and
   OpenTofu reads and changes the bucket's versioning, website, tags and ACL on every plan, so an apply
   can never lock the maintainer out. The runbook's R0 checks that `admin_principal` in
   `terraform.tfvars` is the owner of the key the maintainer applies with. **If a wrong principal was
   applied anyway**, the maintainer can no longer read or change the bucket with that key, and no apply
   repairs it. The way back is the organization's Owner, whom the policy does not limit (the sentence
   quoted above): with an API key of the Owner's own, `aws s3api delete-bucket-policy --bucket <bucket>`
   (or the console's bucket policy tab), then `tofu apply` with the right `admin_principal`. If the Owner
   is out of reach too, only Scaleway support can remove the policy.
2. `DeployWritesAndReadsObjects`: `s3:PutObject` and `s3:GetObject` on `<bucket>/*` to the deploy
   application.
3. `DeployListsTheBucket`: `s3:ListBucket` on `<bucket>` to the deploy application. The deploy does not
   need it to upload; it is granted so the deploy program can list the bucket and fail on a key that is
   in no release's manifest (Hosting decision 5's mitigation for a page a leaked key put outside every
   manifest). Whether IAM lets the application list with only the two object permission sets is checked
   in the runbook (open point 3).
4. `EveryoneReadsTheSite`, only when `var.public_read` is true: `s3:GetObject` on `<bucket>/*` to `*`.

It is applied after the website configuration, since enabling bucket website may apply Scaleway's
default policy ([bucket website](https://www.scaleway.com/en/docs/object-storage/how-to/use-bucket-website/)),
and ours must be the one that stays.

**The read rule (Hosting decision 11) is private first, tested, and public read only if that fails.** A
bucket policy's principal is a `user_id`, an `application_id` or `*`, and no principal stands for Edge
Services. Yet Edge Services' documentation says that with bucket website "objects can remain private"
([create a pipeline](https://www.scaleway.com/en/docs/edge-services/how-to/create-pipeline/)), without
naming the grant that lets it read them. So the private read is tried before the fallback: `public_read`
defaults to `false`, the first apply leaves `*` out of the policy, and the runbook's R5 asks the
pipeline's own endpoint for a probe object. A 200 keeps `public_read = false`, and the site is served
only through the pipeline; anything else sets `public_read = true` in `terraform.tfvars` and applies
again, decision 11's fallback. The default is `false`, not `true`, so that the least exposure is what an
apply gets unless the maintainer has seen it fail, and a missing line in `terraform.tfvars` fails
visibly (the site does not load) rather than silently public. The backend uses website mode, so `/`
answers `index.html`. R5's answer is recorded in milestone 3's hosting ADR, ADR 0042 (open point 8).

**Edge Services lives in the site's project.** The deploy key's policy is scoped to that project, but its
two permission sets hold no Edge Services permission, so the pipeline being there gives it nothing; one
project holds the whole site, and a project-wide look (or a destroy) sees all of it. The cache purge is
the maintainer's, with their own credentials.

### What stays by hand (PRD #297 *Runbook*)

| Item | Why |
|---|---|
| The state bucket | It must exist before `init`; a bootstrap configuration would need a local state of its own (below) |
| The deploy API key, its expiry and rotation | A `scaleway_iam_api_key` resource puts the secret key in the state; `dev/infra-rules.sh` refuses one anywhere under `infra/` |
| The canary check of the key's limits | It needs the key, and it must run against the real service |
| The Namecheap records (www CNAME, apex redirect, CAA, DNSSEC) | Not Scaleway; the provider has no Namecheap resource, and the apex redirect's means is milestone 4's |
| The GitHub `site` environment, its deployment policy and secrets | The secrets are the API key; a GitHub provider would need a token with administration rights |
| Response headers, `http://www` redirect | The provider cannot (below) |
| The cache purge in the recovery steps | A one-off action, not state |

### The state

- **Location:** a private, versioned bucket in a project that is not the site's (the maintainer's admin
  project, or one made for it), in `nl-ams`, key `site/terraform.tfstate`. The deploy key is scoped to
  the site's project, so it can neither read nor write the state. The bucket holds no bucket policy:
  IAM alone decides, and only the maintainer's own key has Object Storage rights in that project.
- **Created by hand, not by a bootstrap configuration.** It is one bucket and one versioning call, made
  once. A bootstrap configuration would need a local state on the admin machine (which
  `dev/infra-rules.sh` refuses, every root having one `s3` backend) or a migration of its own state into
  the bucket it creates; either is more to keep than two commands. The runbook creates it.
- **Locking on:** `use_lockfile = true`. OpenTofu's s3 backend writes a lock object with `If-None-Match`,
  and Scaleway Object Storage supports conditional writes and state locking since 2026-05-26 (its
  changelog, "Conditional writes and Terraform state locking are now supported";
  [using conditional writes](https://www.scaleway.com/en/docs/object-storage/api-cli/using-conditional-writes/)).
  Only one person applies, but a second terminal or a crashed run must not write the state twice.
- **No client-side state encryption** ([state encryption](https://opentofu.org/docs/language/state/encryption/)).
  The state holds no secret by design: IDs, names, the bucket policy and the IAM rule, all of which
  Scaleway holds anyway and none of which grants access. Encryption would protect it only from someone
  who can already read the admin project's buckets, at the price of a passphrase that every `plan` needs
  and whose loss makes the state unreadable (recoverable only by importing every resource again). If the
  state ever has to hold a secret, the decision is revisited: OpenTofu reads the encryption configuration
  from `TF_ENCRYPTION` as well, so it can be turned on with a `pbkdf2` passphrase from the maintainer's
  password manager without the offline checks needing it.

### What the provider cannot do at v2.93.0

The Edge Services resources are `pipeline`, `plan`, `head_stage` and the `backend`, `cache`, `tls`,
`dns`, `route` and `waf` stages
([docs](https://github.com/scaleway/terraform-provider-scaleway/tree/v2.93.0/docs/resources)). None has an
argument for:

- **Response headers** (Hosting decision 2): no header, CSP or HSTS setting on any stage. Scaleway's
  [FAQ](https://www.scaleway.com/en/docs/edge-services/faq/) lists none either. The site keeps its
  `<meta>` CSP and framing guard; HSTS stays milestone 4's go/no-go, checked by hand there.
- **Redirecting or refusing plain HTTP on `www`** (decision 3): the DNS and TLS stages have no redirect
  or HTTPS-only switch. What `http://www.shift2eu.com` answers is checked by hand after the apply
  (runbook) and decided in milestone 4.
- **A grant to Edge Services** (decision 11): no Edge principal in a bucket policy. Whether the
  website-mode backend reads private objects without one is R5's test; `public_read` is the fallback.
- **Compression and content types** (decision 4): nothing to set; checked by hand in milestone 4.
- **A one-off cache purge:** the cache stage's `purge_requests` makes a purge part of the configuration;
  the runbook purges with the Scaleway CLI or console instead.

### The origins (Hosting decision 11)

With either read rule, the site is served by:

- `https://www.shift2eu.com`: the pipeline, the only origin attested and checked.
- The pipeline's own endpoint (the DNS stage's `default_fqdn`, `<id>.svc.edge.scw.cloud`): the same
  pipeline.

With `public_read` only, also by:

- The bucket website endpoint `<bucket>.s3-website.<region>.scw.cloud`.
- The bucket's S3 endpoints (`<bucket>.s3.<region>.scw.cloud/<key>` and
  `s3.<region>.scw.cloud/<bucket>/<key>`), object by object, without the index document.

R5 asks every one of them, over HTTPS and plain HTTP, and the answers are recorded in milestone 3's
hosting ADR, ADR 0042: under the private rule the last two must refuse the probe object.

### How the PRD's *Runbook* changes

Its creation steps are now: create the state bucket, fill `backend.hcl` and `terraform.tfvars`,
`dev/tofu.sh init`, `plan`, `apply` on the admin machine with the maintainer's own credentials, and read
the outputs. The API key, the canary check, DNS, the GitHub environment, the records, the origin checks
and the recovery steps stay command blocks the maintainer runs by hand. Removing the site deletes the
CNAME first, then `tofu destroy`, which keeps the bucket while it holds objects (no `force_destroy`).

## Alternatives considered

- **Terraform.** Its licence (BSL) and HashiCorp's registry; OpenTofu is the maintainer's choice, open
  source and reads the same provider.
- **Keep the console steps.** Nothing would hold the bucket and IAM policies after the day they were
  typed; the offline tests now do.
- **Public read from the start (`public_read` true by default).** Simpler, and it is what decision 11
  falls back to, but it would expose the bucket's endpoints before anyone had seen whether the private
  read works.
- **A public-read ACL.** It makes every object, present and future, public outside the policy, and the
  acl attribute is deprecated. `dev/infra-rules.sh` refuses any `scaleway_object_bucket_acl` and any
  `acl` argument, even `private`, so the bucket policy is the only read rule.
- **The API key in OpenTofu.** Its secret would be in the state.
- **A bootstrap configuration for the state bucket; state encryption.** Above.
- **Edge Services in its own project.** Equally safe for the key, but two projects for one site, and a
  look at the site's project would not show what serves it.

## Consequences

- The bucket policy, the IAM rule and the pipeline's chain are pinned by `infra/site/tests` and
  re-checked on every `make check`; a change to them shows in the maintainer's `plan`.
- The maintainer needs, on the admin machine: a checkout of the repository, their own Scaleway API key
  (in `SCW_*` for the provider and `AWS_*` for the backend), and the `scw` and `aws` CLIs for the hand
  steps.
- With `public_read`, objects are public through four origins; without it, through the pipeline's two.
  Only `www` is attested.

### Open points for the maintainer

1. **`admin_principal`:** your own `user_id:` (recommended, when the key you apply with is yours) or an
   admin application's `application_id:`. If you are the organization's Owner you can always rewrite the
   policy, but OpenTofu still needs the statement to read the bucket. R0 checks it against your key; the
   lock-out recovery is under *The bucket policy* above.
2. **Plain HTTP on `www` and on the other origins** (decisions 3 and 11): checked after the apply; if
   `http://www` serves 200, milestone 4 is a no-go until Edge Services can redirect or refuse it.
3. **`s3:ListBucket` for the deploy key:** if the runbook's list probe answers 403, IAM does not grant
   it with the two object permission sets; the statement then does nothing and the deploy program does
   not list. Recommended: keep the statement, record the answer.
4. **State encryption:** recommended off (above); say if you want it on.
5. **Bucket name:** `shift2eu-site-www` by default; bucket names are global, so set another in
   `terraform.tfvars` if it is taken.
6. **The Starter plan in OpenTofu:** a `destroy` ends the subscription too. Recommended: keep it in, so the
   project is complete; recheck the price first (the runbook does).
7. **Scaleway CLI commands in the runbook** (`scw iam api-key create`, the cache purge): check each
   against `scw <command> --help` of the installed version on first use.
8. **`public_read`:** off by default (above). If R5's probe through the pipeline answers 200, keep it off;
   otherwise set it true and apply again. Record the answer, and whether the bucket's own endpoints
   refuse the probe while it is off, in milestone 3's hosting ADR, ADR 0042. Recommended: off if R5 allows it.
9. **AWS CLI checksums:** R0 sets `AWS_REQUEST_CHECKSUM_CALCULATION` and
   `AWS_RESPONSE_CHECKSUM_VALIDATION` to `when_required`, so AWS CLI v2 sends and checks a checksum only
   where the S3 API requires one, as before its 2.23 release
   ([AWS SDKs: data integrity protections](https://docs.aws.amazon.com/sdkref/latest/guide/feature-dataintegrity.html)).
   Scaleway's documentation (scaleway/docs-content, read 2026-11-21) says nothing about these settings; its
   [supported checksums](https://www.scaleway.com/en/docs/object-storage/reference-content/supported-checksums/)
   include the CRC64NVME the new default uses, so they may be unneeded. Kept, as they change no
   permission and rule out a checksum mismatch hiding as a probe's INCONCLUSIVE answer.
