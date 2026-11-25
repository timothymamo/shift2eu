# The site runbook

The maintainer's runbook for the site's hosting: the Scaleway side OpenTofu creates from `infra/site/`
and the steps that stay by hand (the state bucket, the deploy key and its limits, the origin checks, the
Namecheap records, the GitHub `site` environment, the deploy switch), with the key's rotation, the
recovery and the teardown ([PRD #297](../prds/297-web-assessment-site.md), *Runbook*;
[ADR 0039](adr/0039-create-the-sites-scaleway-hosting-with-opentofu.md),
[ADR 0042](adr/0042-host-the-site-behind-edge-services-and-deploy-it-from-the-release-with-a-bucket-scoped-key.md),
the hosting ADR the steps record their results in). Written in milestone 3, followed in milestone 4, on
mac-mini-home, the admin machine ([Development environment](dev-environment.md)). Agents never run it.

*The front (milestone 4):* a Serverless Container running the pinned Caddy image serves `www` and the
apex in front of the Edge Services pipeline
([ADR 0045](adr/0045-front-the-site-with-a-serverless-container-running-a-pinned-caddy.md)). Its custom
domains need their DNS records first, so it comes up in stages: R4's apply creates the container (and,
on a pipeline created before it, removes the pipeline's `www` domain and TLS stage), R8 points the `www`
CNAME and the apex ALIAS at the container, applies again with `front_domains = true`, waits for both
domains to be *ready* and runs the live probes ADR 0045 requires before the site goes live. R8 ends
with the rollback.

Placeholders in `<...>` are the maintainer's: `<state-bucket>` the state bucket, `<owner>` the
repository's owner; `$REGION`, `$ADMIN_PROJECT` (your key's default project, where the state is) and
`$ME` (your key's owner, as a bucket-policy principal) come from R0; `$P`, `$A`, `$B`, `$E`, `$PL`,
`$NS` and `$CE` are `apply`'s outputs (R4): a later shell runs R0, `init` and R4's output lines first. Every block runs
in bash, which R0's first line starts: macOS's login shell is zsh, whose `read` takes other options.
Secrets come from the password manager into the shell with `read -s`, never into a file or the shell
history. Scratch files go in a `mktemp -d` directory, never in the checkout. The `scw` and `aws`
commands are checked against their `--help` on first use (ADR 0039, open point 7). Stop at the first
`PASS` that does not hold.

## R0. The admin machine

mac-mini-home, in a checkout of the repository at `main`'s head.

```bash
bash                                              # every block below runs in this bash, not in zsh
echo "$BASH_VERSION"                              # PASS: a version, such as 3.2.57(1)-release
# FAIL no output: still zsh; run bash
git pull --ff-only && git status --short          # PASS: no output
dev/tofu.sh version                               # PASS: first line "OpenTofu v1.13.9"
# FAIL "refusing to run it": the tarball does not match infra/tofu.yaml's SHA-256; stop, never run another tofu
aws --version && scw version && jq --version      # PASS: each prints a version
# FAIL "command not found": brew install awscli scw jq
# Your own Scaleway API key (not the deploy key), for this shell only
read -rp 'SCW access key: ' SCW_ACCESS_KEY && read -rsp 'SCW secret key: ' SCW_SECRET_KEY && echo
export SCW_ACCESS_KEY SCW_SECRET_KEY AWS_ACCESS_KEY_ID="$SCW_ACCESS_KEY" AWS_SECRET_ACCESS_KEY="$SCW_SECRET_KEY"
# The region: terraform.tfvars's region if it sets one, else variables.tf's default
REGION="$(sed -n 's/^region *= *"\([a-z-]*\)".*/\1/p' infra/site/terraform.tfvars 2>/dev/null)"
REGION="${REGION:-$(sed -n '/^variable "region"/,/^}/s/^ *default *= *"\([a-z-]*\)".*/\1/p' infra/site/variables.tf)}"
echo "$REGION"                                    # PASS: fr-par, nl-ams or pl-waw
export AWS_DEFAULT_REGION="$REGION" AWS_ENDPOINT_URL="https://s3.$REGION.scw.cloud"
# AWS CLI v2 sends and checks checksums only where the S3 API requires one (ADR 0039, open point 9)
export AWS_REQUEST_CHECKSUM_CALCULATION=when_required AWS_RESPONSE_CHECKSUM_VALIDATION=when_required
# (.APIKey // .): a current scw wraps the key in "APIKey", an older one prints it unwrapped
ADMIN_PROJECT="$(scw iam api-key get "$SCW_ACCESS_KEY" -o json | jq -r '(.APIKey // .) | .default_project_id')"
ME="$(scw iam api-key get "$SCW_ACCESS_KEY" -o json | jq -r '(.APIKey // .) | if ((.user_id // "") | length) > 0 then "user_id:" + .user_id elif ((.application_id // "") | length) > 0 then "application_id:" + .application_id else empty end')"
echo "$ADMIN_PROJECT $ME"                         # PASS: your admin project's ID (never the site's) and "user_id:<your user ID>"
# FAIL the site's project ID (an R5, R7 or R12 left unfinished): scw iam api-key update "$SCW_ACCESS_KEY" default-project-id=<admin project ID>, then R0 again
# FAIL an empty value, "null", or "user_id:" or "application_id:" with nothing after it: the scw call failed or its JSON changed shape (a wrong key, no network, a new scw); fix it, then R0 again
# The bucket policy's admin statement must be you (ADR 0039, The bucket policy); the first time, after R2
AP="$(sed -n 's/^admin_principal *= *"\(.*\)".*/\1/p' infra/site/terraform.tfvars 2>/dev/null)"
[ -n "$ME" ] && [ -n "$AP" ] && [ "$AP" = "$ME" ] && echo "PASS admin_principal is $ME" || echo "FAIL admin_principal is '$AP', \$ME is '$ME'"
# FAIL: an empty $ME: R0's scw lines again; an empty admin_principal (or no terraform.tfvars): set admin_principal = "<the value of $ME>" in infra/site/terraform.tfvars (R2) before any plan; a wrong one locks you out of the bucket
```

## R1. The state bucket

Once; your admin project, never the site's (ADR 0039).

```bash
aws s3api create-bucket --bucket <state-bucket>                              # PASS: prints its Location
aws s3api put-bucket-versioning --bucket <state-bucket> --versioning-configuration Status=Enabled
aws s3api get-bucket-versioning --bucket <state-bucket>                      # PASS: "Status": "Enabled"
aws s3api get-bucket-acl --bucket <state-bucket> --query 'Grants[].Grantee'  # PASS: only your own ID
# FAIL "AllUsers": aws s3api put-bucket-acl --bucket <state-bucket> --acl private
aws s3api get-bucket-policy --bucket <state-bucket>                          # PASS: NoSuchBucketPolicy (IAM alone decides)
```

## R2. The inputs

Git-ignored files, IDs only, no secret.

```bash
cp infra/site/backend.hcl.example infra/site/backend.hcl             # bucket = "<state-bucket>", region and endpoint for "$REGION"
cp infra/site/terraform.tfvars.example infra/site/terraform.tfvars   # organization_id, admin_principal = the value of "$ME"; leave public_read out (false)
git status --short --ignored infra/site                              # PASS: exactly three ignored lines: infra/site/backend.hcl, infra/site/terraform.tfvars and (after the first init) infra/site/.terraform/
# FAIL a line starting "??" (an override.tf or another stray file: delete it) or another ignored line (a plan or state file: delete it)
# R0's region lines again if you set region, and R0's last line: PASS admin_principal
# Note: make infra-check's tofu test now reads this terraform.tfvars too (CI runs with the defaults); never add a *.auto.tfvars ([tfvars-file] refuses it)
# Note: tofu loads terraform.tfvars, then terraform.tfvars.json, then every *.auto.*: keep exactly one of terraform.tfvars or terraform.tfvars.json here (a stray .json would override the .tfvars unseen)
```

## R3. The price

Before `apply` takes out the plan (PRD #297, Hosting decision 9).

```bash
open 'https://www.scaleway.com/en/pricing/network/#edge-services'   # PASS: Starter still €0.99/month, 1 pipeline
# FAIL "another price or limit": stop, and update Hosting decision 9 first
```

## R4. Create the hosting

And after every change to `infra/site/` merged to `main`.

The pipeline holds no TLS stage and no custom domain (ADR 0045): the container in front of it holds
`www` and the apex, created with `front_domains` off here and on in R8. Only R8's rollback,
`pipeline_www = true` in `terraform.tfvars` and this apply, re-adds the TLS stage and the pipeline's
`www` name; the stage requests its managed certificate once, at its creation:
after that rollback, R8's recreate step is expected (issue #541).

An apply that removes the TLS stage (`pipeline_www = false` on a pipeline that still has one: milestone
3's hosting, or the end of R8's rollback) must update the DNS stage before it destroys the stage it
referenced: Scaleway refuses to delete a TLS stage a DNS stage references. OpenTofu orders it so only
when the TLS stage's state carries `create_before_destroy: true`, since a removed instance's destroy
reads that flag from the state, not the configuration; a stage written to the state before the flag
was in the configuration has none, and its destroy then runs first and fails (issue #595). The check
after the plan below reads the flag.

```bash
dev/tofu.sh -chdir=infra/site init -backend-config=backend.hcl -lockfile=readonly   # PASS: "OpenTofu has been successfully initialized"
grep -c '^front_domains *= *true' infra/site/terraform.tfvars   # PASS: 0 before R8's second apply, 1 after it
# Before an apply that removes the TLS stage: the baseline of the pipeline's default endpoint, with its TLS stage still there
E="$(dev/tofu.sh -chdir=infra/site output -raw edge_endpoint)"; curl -sS -o /dev/null -w 'baseline %{http_code} verify=%{ssl_verify_result}\n' "https://$E/"   # record it: the answer check 1 below compares with once the TLS stage is gone
dev/tofu.sh -chdir=infra/site plan -out=site.tfplan        # PASS (first time): "Plan: 14 to add, 0 to change, 0 to destroy."
# PASS (a plan that removes the TLS stage): the DNS stage updated in place and the TLS stage destroyed; on milestone 3's hosting with the namespace and the container added, "Plan: 2 to add, 1 to change, 1 to destroy.", at the end of R8's rollback "Plan: 0 to add, 1 to change, 1 to destroy." (plus the container's domains with front_domains = true)
# PASS (if the provider replaces the DNS stage): the same plan with the DNS stage "must be replaced": its default_fqdn changes, so $E changes; if the new stage's default_fqdn is still empty at apply, the container's precondition fails and the container keeps its old UPSTREAM until the second pass below (a short outage window after R10); nothing serves visitors before R10, so apply it, read $E again from the output lines below, and record it in ADR 0039's container amendment
# FAIL any other resource replaced or destroyed: stop, apply nothing, rm infra/site/site.tfplan, and find out why
# The TLS stage's create_before_destroy as the state holds it: the flag a removal's destroy reads (issue #595); run it before applying any plan that destroys the TLS stage
dev/tofu.sh -chdir=infra/site state pull | jq -c '[.resources[] | select(.type == "scaleway_edge_services_tls_stage") | .instances[] | .create_before_destroy]'   # PASS: [] or nothing (no state yet, or no TLS stage), or [true]
# FAIL [null] or [false]: the stage's state predates the flag, and the apply would destroy it before updating the DNS stage; apply nothing, rm infra/site/site.tfplan, write the flag into the state with the stage still in the configuration, then plan again from the plan line above:
dev/tofu.sh -chdir=infra/site apply -var=pipeline_www=true -refresh-only   # only after that FAIL; it changes no resource, only the state (answer "yes" if it asks); -var sets pipeline_www = true for this run alone and terraform.tfvars keeps false, so the plan after it removes the stage again
# PASS: the state check line above, run again, reads [true]; FAIL still [null] or [false]: apply nothing, and take the route under the apply line's second 500 FAIL below
dev/tofu.sh -chdir=infra/site show site.tfplan | less      # PASS: the bucket policy's three statements, four with public_read (ADR 0039), the IAM rule's two permission sets in one project; the container's image "docker.io/library/caddy:<tag>@sha256:...", max_scale 1, cpu_limit 140, memory_limit_bytes 268000000 (the 0.25 GB the platform keeps; issue #596), min_scale 0, its UPSTREAM the DNS stage's default_fqdn
dev/tofu.sh -chdir=infra/site apply site.tfplan            # PASS: "Apply complete"
# FAIL (expected on a first setup, or when the DNS stage is replaced) "Resource precondition failed" on scaleway_container.site, "default_fqdn is still empty": the rest is applied; go on with the wait loop, then the second pass below
# FAIL "Resource postcondition failed" on scaleway_container.site: the platform kept a redirect or a warm instance the provider did not send; set plain HTTP "enabled" and min scale 0 in the console (Serverless Containers, the namespace shift2eu-site, front, Settings), then plan again: PASS "No changes."
# FAIL "Error acquiring the state lock": another tofu holds it; wait, or after a crash dev/tofu.sh -chdir=infra/site force-unlock <lock ID>
# FAIL a bare 500 on scaleway_edge_services_tls_stage.site[0] while destroying it (the destroy ran before the DNS stage's update): re-plan before retrying: the old plan no longer matches the state; rm infra/site/site.tfplan, run the state check and its refresh-only line above, then the plan line again, and apply that plan
# FAIL the 500 again, or the state check still reads [null] after the refresh-only line: take the TLS stage out of the state, apply the DNS stage's update alone, and delete the stage by hand (the next nine lines, only after this FAIL)
T="$(mktemp -d)"; dev/tofu.sh -chdir=infra/site state pull > "$T/state-backup.json"; jq -r '.serial' "$T/state-backup.json"   # PASS: a number (the backup; keep it until the last line below)
TLS="$(jq -r '.resources[] | select(.type == "scaleway_edge_services_tls_stage") | .instances[0].attributes.id // empty' "$T/state-backup.json")"; TLS="${TLS##*/}"; echo "$TLS"   # PASS: the TLS stage's ID, a UUID (36 characters), not empty and not null; FAIL anything else: stop, remove nothing
dev/tofu.sh -chdir=infra/site state rm 'scaleway_edge_services_tls_stage.site[0]'   # PASS: "Removed scaleway_edge_services_tls_stage.site[0]" and "Successfully removed 1 resource instance(s)."
dev/tofu.sh -chdir=infra/site plan -out=site.tfplan        # PASS: "Plan: 1 to add, 1 to change, 0 to destroy." (the container added, and the DNS stage updated: cache_stage_id set, tls_stage_id gone, fqdns []; "0 to add" if the failed apply created the container); FAIL any destroy or replace: stop, apply nothing, and find out why
dev/tofu.sh -chdir=infra/site apply site.tfplan            # PASS: "Apply complete"; then rm infra/site/site.tfplan
PL="$(dev/tofu.sh -chdir=infra/site output -raw pipeline_id)"; scw edge-services tls-stage list pipeline-id="$PL" -o json | jq -c '[.[].id]'   # PASS: ["<the $TLS value>"], the orphan alone; FAIL anything else: stop, do not delete
scw edge-services tls-stage delete "$TLS"                  # PASS: no error (or delete it in the console: Edge Services, the pipeline, its TLS stage)
scw edge-services tls-stage list pipeline-id="$PL" -o json | jq -c '[.[].id]'   # PASS: []
dev/tofu.sh -chdir=infra/site plan -detailed-exitcode && rm -r "$T"   # PASS: exit 0, "No changes." (the backup removed); then go on with the wait loop below; FAIL exit 2: read the diff, and keep "$T/state-backup.json" until it is explained
# Scaleway assigns the DNS stage's default_fqdn after creating it: edge_endpoint is empty right after apply, so refresh the state (no resource change) until it is set, 20 tries 30 s apart
# Watch the first iteration: with the container absent, refresh-only should not trip the precondition, and the container_endpoint output reads "" (try()) rather than failing (not verifiable offline)
n=0; refresh_failed=0; until [ -n "$(dev/tofu.sh -chdir=infra/site output -raw edge_endpoint 2>/dev/null)" ] || [ "$n" -ge 20 ]; do sleep 30; dev/tofu.sh -chdir=infra/site apply -refresh-only -auto-approve || { refresh_failed=1; break; }; n=$((n + 1)); done
if [ "$refresh_failed" -eq 1 ]; then echo "FAIL refresh-only failed: read the error above"; elif [ -n "$(dev/tofu.sh -chdir=infra/site output -raw edge_endpoint 2>/dev/null)" ]; then echo "PASS edge_endpoint is set"; else echo "FAIL edge_endpoint still empty after 10 minutes"; fi
# FAIL refresh-only failed: a state lock: as the apply line's FAIL says; a key error: R0; any other error: fix its cause, then run the wait loop and its check again
# FAIL edge_endpoint still empty: first an error printed above (a state lock: as the apply line's FAIL says; a key error: R0), else check the pipeline's DNS stage in the console (Edge Services) for its default FQDN; then run the wait loop and its check again
# The second pass: run the next two lines only after the apply line's precondition FAIL; otherwise skip them
dev/tofu.sh -chdir=infra/site plan -out=site.tfplan        # PASS (second pass, first setup): "Plan: 1 to add, 0 to change, 0 to destroy." (the container, its UPSTREAM $E)
# PASS (second pass, the DNS stage was replaced): "Plan: 0 to add, 1 to change, 0 to destroy." (only the container's UPSTREAM, set to $E)
dev/tofu.sh -chdir=infra/site apply site.tfplan            # PASS (second pass): "Apply complete"; FAIL the precondition again: edge_endpoint is still empty, run the wait loop and its check again
dev/tofu.sh -chdir=infra/site plan -detailed-exitcode      # PASS: exit 0, "No changes."
# FAIL exit 2: read the diff; the provider or someone changed a resource; one "to add" after the refresh was dropped from the state by the refresh: check in the console whether it still exists; if it does, import it; if it is really gone, find out why before re-creating it
dev/tofu.sh -chdir=infra/site output                       # PASS: project_id, deploy_application_id, bucket_name, bucket_website_endpoint, pipeline_id, edge_endpoint, container_namespace_id, container_endpoint, front_domains ([] before R8's second apply)
aws s3api list-objects-v2 --bucket <state-bucket> --query 'Contents[].Key'   # PASS: "site/terraform.tfstate" and no .tflock left over
rm infra/site/site.tfplan
# The outputs the later steps use, read now while your key's default project is the state's
P="$(dev/tofu.sh -chdir=infra/site output -raw project_id)"; A="$(dev/tofu.sh -chdir=infra/site output -raw deploy_application_id)"
B="$(dev/tofu.sh -chdir=infra/site output -raw bucket_name)"; E="$(dev/tofu.sh -chdir=infra/site output -raw edge_endpoint)"
PL="$(dev/tofu.sh -chdir=infra/site output -raw pipeline_id)"; echo "$P $A $B $E $PL"   # PASS: five values, none empty
NS="$(dev/tofu.sh -chdir=infra/site output -raw container_namespace_id)"; CE="$(dev/tofu.sh -chdir=infra/site output -raw container_endpoint)"; CE="${CE#https://}"; CE="${CE%/}"
echo "$NS $CE"                                             # PASS: the namespace ID and "shift2eu-site-<...>-front.functions.fnc.$REGION.scw.cloud" (or the like), none empty
[ -n "$CE" ] && echo "PASS container_endpoint is set" || echo "FAIL container_endpoint is empty: the container does not exist yet; run R4's second pass"
[ -n "$P" ] && [ "$P" != "$ADMIN_PROJECT" ] && echo "PASS the site's project is not the admin project" || echo "FAIL the site's project is the admin project"
# FAIL: R0's ADMIN_PROJECT line ran while your key's default project was the site's; fix it as R0 says, then R0 again
# After a change to the IAM or the bucket policy: R7 again
```

The checks after every apply (the scratch directory first):

```bash
W="$(mktemp -d)"
# 1. The pipeline's default endpoint over HTTPS with no TLS stage (unverified before the first apply after ADR 0045)
curl -sS -o /dev/null -w '%{http_code} verify=%{ssl_verify_result}\n' "https://$E/"   # PASS: "verify=0" and a status (200, 403 or 404 from the bucket); record it in ADR 0045's amendment
# FAIL a TLS or connection error: the pipeline serves no HTTPS without a TLS stage; the container cannot fetch the site: R8's rollback, then record it in ADR 0045
# 2. The container: ready, and running the image by its digest (ready also means the Containers API took the Caddyfile, about 5.2 KiB, as args[2]; record it in ADR 0045)
scw container container list namespace-id="$NS" region="$REGION" -o json | jq -r '.[] | "\(.name) \(.status) \(.registry_image)"'   # PASS: exactly "front ready docker.io/library/caddy:<tag>@sha256:<the digest in infra/site/container.tf>"
# FAIL "error" with an error_message, or a reference without @sha256: record it; ADR 0045's registry mirror is then the way, recorded there before it is taken
# A failed start is read from the container's startup log (Cockpit, the container's logs; nothing of a visitor is in it, ADR 0045). Expected there, and no failure: Caddy's warning "Unnecessary header_up Host ...": the Caddyfile writes Caddy's default for an https:// upstream out on purpose
# 3. Warm it (min_scale 0): one request, its answer discarded, its time recorded against the live check's 30 s
curl -s -o /dev/null -w '%{http_code} %{time_total}s\n' --max-time 60 "http://$CE/"   # PASS: "308" and a time; record the time in ADR 0045's amendment
# FAIL over 30 s: the cold start would fail the live check's first request; record it, and R8 warms the container right before every live check
curl -s -o /dev/null -w '%{http_code} %{redirect_url}\n' "http://$CE/a?b=1"      # PASS: "308 https://www.shift2eu.com/a?b=1"
curl -s -o /dev/null -w '%{http_code} %{redirect_url}\n' "https://$CE/a?b=1"     # PASS: "308 https://www.shift2eu.com/a?b=1" (HTTPS, but the endpoint is not www)
rm -r "$W"
```

**The digest pull go/no-go**, once, after the first apply that creates the container (ADR 0045): a throwaway
container in the same namespace, given the pinned tag with a wrong digest, must fail to start. Check the
flags against `scw container container create --help` first (ADR 0039, open point 7).

```bash
BAD="docker.io/library/caddy:2.11.9-alpine@sha256:$(printf '0%.0s' $(seq 1 64))"; echo "$BAD"   # PASS: the tag, then 64 zeros
PR="$(scw container container create namespace-id="$NS" region="$REGION" name=digest-probe registry-image="$BAD" min-scale=0 max-scale=1 cpu-limit=140 memory-limit-bytes=268000000 -o json | jq -r '.id')"; echo "$PR"   # PASS: an ID
scw container container deploy "$PR" region="$REGION" >/dev/null   # PASS: no error (a create that deploys by itself may answer that it is already deploying: fine)
n=0; until scw container container list namespace-id="$NS" region="$REGION" name=digest-probe -o json | jq -e '.[0].status | IN("error","ready")' >/dev/null || [ "$n" -ge 30 ]; do sleep 20; n=$((n + 1)); done
scw container container list namespace-id="$NS" region="$REGION" name=digest-probe -o json | jq -r '.[0] | "\(.status) \(.error_message)"'   # PASS: "error" with a pull or manifest error: the platform pulls by digest; record it in ADR 0045
# FAIL "ready": the platform ignored the digest; record it, and take ADR 0045's registry mirror (recorded there first) before R8
# On any FAIL or stop in this block, the two lines below first: the probe never stays in the namespace (an empty $PR in a fresh shell: scw container container list namespace-id="$NS" region="$REGION" name=digest-probe -o json | jq -r '.[].id')
scw container container delete "$PR" region="$REGION"   # PASS: no error
scw container container list namespace-id="$NS" region="$REGION" -o json | jq -r '.[].name'   # PASS: "front" alone
```

## R5. The read rule and the origins

PRD #297 Hosting decision 11, ADR 0039 open point 8; recorded in the hosting ADR.

```bash
# Your own key's S3 calls land in its default project; the site's bucket is in the site's project (until R7's step 8)
scw iam api-key update "$SCW_ACCESS_KEY" default-project-id="$P"
W="$(mktemp -d)"; K="origin-probe-$(date +%s).txt"     # scratch files outside the checkout; a new key each time, never one the cache holds
echo probe > "$W/$K" && aws s3api put-object --bucket "$B" --key "$K" --body "$W/$K" --content-type text/plain
# FAIL "NoSuchBucket": the key's default project is not the site's; rerun the scw iam api-key update above
# The private read first: public_read is false (the default) after the first apply. Only the bucket's own
# access-denied answer means Edge Services cannot read the private objects: a 403 with the S3 API's XML body
# (<Code>AccessDenied</Code>) or the bucket website endpoint's HTML page (<h1>403 Forbidden</h1> and the list item
# <li>Code: AccessDenied</li>). Edge Services' backend stage reads the bucket's website endpoint, so the HTML form is
# the one it relays. Anything else (5xx, 404, 000, a TLS error, the edge's own error page) is most likely the pipeline
# or its certificate still provisioning: retry for up to 30 minutes, every minute
edge() { curl -sS -o "$W/edge-body" -D "$W/edge-head" -w '%{http_code}' "https://$E/$K" 2>"$W/edge-err"; }
for i in $(seq 1 30); do
  C="$(edge)"; [ "$C" = 200 ] && break
  [ "$C" = 403 ] && grep -q -e '<Code>AccessDenied</Code>' -e '<li>Code: AccessDenied</li>' "$W/edge-body" && break
  echo "try $i: ${C:-000} $(cat "$W/edge-err" "$W/edge-body" 2>/dev/null | LC_ALL=C tr -c '[:print:]' ' ' | cut -c1-200)"; sleep 60
done
echo "status $C"; grep -i -E '^(server|via|x-amz-request-id|x-cache|content-type):' "$W/edge-head"; head -c 400 "$W/edge-body" | LC_ALL=C tr -c '[:print:]\n' ' '; echo
# Status, headers and body tell the edge's own error from the bucket's: an x-amz-request-id header, an S3 XML
#   <Code>AccessDenied</Code> body or the website endpoint's HTML page with <li>Code: AccessDenied</li> come from the bucket
# PASS 200 with public_read false: Edge Services reads the private objects; keep public_read false and record it
# PASS 200 with public_read true (the second run, after the 403 outcome below): the recorded fallback, the site is
#   served publicly; record it as the fallback in the hosting ADR
# 403 AccessDenied from the bucket (either form, XML or HTML): Edge Services cannot read the private objects; record status, headers and body, then:
#   aws s3api delete-object --bucket "$B" --key "$K"; scw iam api-key update "$SCW_ACCESS_KEY" default-project-id="$ADMIN_PROJECT";
#   add public_read = true to infra/site/terraform.tfvars;
#   R4 (its plan adds only the EveryoneReadsTheSite statement), then R5 again from its first line: PASS 200
# FAIL any other answer after 30 tries: record status, headers, body and the try lines; leave public_read as it is,
#   aws s3api delete-object --bucket "$B" --key "$K"; scw iam api-key update "$SCW_ACCESS_KEY" default-project-id="$ADMIN_PROJECT";
#   and stop until the pipeline's state (scw edge-services pipeline get "$PL") and the answer are explained
for u in "https://$E" "http://$E" "https://$B.s3-website.$REGION.scw.cloud" "http://$B.s3-website.$REGION.scw.cloud" \
  "https://$B.s3.$REGION.scw.cloud" "http://$B.s3.$REGION.scw.cloud"; do
  printf '%s ' "$u"; curl -s -o /dev/null -w '%{http_code} %{redirect_url}\n' "$u/$K"
done   # PASS: every line recorded in the hosting ADR, with public_read's value
# public_read false: the four bucket lines answer 403; public_read true: all serve it; per http:// line: 200, a redirect, or refused
# FAIL public_read false and a bucket line answers 200: the bucket is public some other way; stop, and check the bucket policy and ACL
# The container's own endpoint is an origin too (ADR 0045): it never serves the site, only Caddy's 308 to www
for u in "https://$CE" "http://$CE"; do printf '%s ' "$u"; curl -s -o /dev/null -w '%{http_code} %{redirect_url}\n' "$u/$K"; done   # PASS: both "308 https://www.shift2eu.com/$K"
# FAIL a 200: the container proxies a name other than www; stop, and check infra/site/Caddyfile's @www against main
aws s3api delete-object --bucket "$B" --key "$K" && rm -r "$W"
```

## R6. The deploy API key

By hand: its secret never reaches the state.

```bash
scw iam api-key create application-id="$A" default-project-id="$P" \
  expires-at="$(date -u -v+90d +%Y-%m-%dT%H:%M:%SZ)" description="shift2eu site deploy"
# PASS: prints access_key and secret_key once; copy both into the password manager only, never a file
# FAIL "permission denied": your key needs IAMManager in the organization
# Or pipe it straight in, so the secret never reaches the screen: scw iam api-key create ... -o json | <your password manager's CLI>
clear && printf '\033[3J'             # the secret off the screen and out of the scrollback (Terminal: Cmd-K as well)
scw iam api-key list bearer-id="$A"   # PASS: one key, with its expires_at
```

## R7. The deploy key's limits

The canary step; after R6, every rotation and every change to the IAM or bucket policy. What it must
verify is the PRD's acceptance criterion ([PRD #297](../prds/297-web-assessment-site.md), *Runbook*,
the R7 box); its result goes in the hosting ADR, with the ListBucket answer that decides the deploy's
`-list-bucket` in milestone 4.

```bash
CB="$B-canary"; W="$(mktemp -d)"   # scratch files outside the checkout
# 1. Own key (default project the site's, R5): save the site bucket's configuration; R4's apply restores website, versioning and policy, the ACL is restored below
aws s3api get-bucket-website --bucket "$B" > "$W/saved-website.json"
aws s3api get-bucket-versioning --bucket "$B" > "$W/saved-versioning.json"   # PASS: "Status": "Enabled"
aws s3api get-bucket-acl --bucket "$B" > "$W/saved-acl.json"
aws s3api get-bucket-policy --bucket "$B" --query Policy --output text > "$W/saved-policy.json"
# 2. Own key: the canary bucket, of the site bucket's shape
scw object bucket create name="$CB" region="$REGION" project-id="$P" enable-versioning=true   # PASS: prints the bucket
aws s3api put-bucket-website --bucket "$CB" --website-configuration "file://$W/saved-website.json"
sed "s/\"$B/\"$CB/g" "$W/saved-policy.json" > "$W/canary-policy.json" && aws s3api put-bucket-policy --bucket "$CB" --policy "file://$W/canary-policy.json"
# 3. The deploy key, for this shell only
read -rp 'deploy access key: ' AWS_ACCESS_KEY_ID && read -rsp 'deploy secret key: ' AWS_SECRET_ACCESS_KEY && echo && export AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY
probe() { if out="$(aws "$@" 2>&1)"; then echo "FAIL allowed: $*"; elif [[ ${out} == *AccessDenied* ]]; then echo "PASS 403: $*"; else echo "INCONCLUSIVE: $* -> ${out}"; fi; }
# For a bucket in another project: the key's S3 calls go to its default project (the site's), so such a bucket may answer NoSuchBucket, which denies it as well
xprobe() { if out="$(aws "$@" 2>&1)"; then echo "FAIL allowed: $*"; elif [[ ${out} == *AccessDenied* ]]; then echo "PASS 403: $*"; elif [[ ${out} == *NoSuchBucket* ]]; then echo "PASS NoSuchBucket (outside the key's project): $*"; else echo "INCONCLUSIVE: $* -> ${out}"; fi; }
echo canary > "$W/probe.txt"
V="$(aws s3api put-object --bucket "$B" --key canary/probe.txt --body "$W/probe.txt" --query VersionId --output text)"; echo "$V"   # PASS: a version ID (PutObject allowed)
aws s3api list-objects-v2 --bucket "$B" --max-items 1 >/dev/null && echo "ListBucket allowed" || echo "ListBucket denied"   # record it (ADR 0039 open point 3)
# 4. Re-submissions on the site bucket
probe s3api put-bucket-website --bucket "$B" --website-configuration "file://$W/saved-website.json"
probe s3api put-bucket-versioning --bucket "$B" --versioning-configuration "file://$W/saved-versioning.json"
probe s3api put-bucket-acl --bucket "$B" --access-control-policy "file://$W/saved-acl.json"
probe s3api put-bucket-policy --bucket "$B" --policy "file://$W/saved-policy.json"
# 5. The canary object only
probe s3api delete-object --bucket "$B" --key canary/probe.txt
probe s3api delete-object --bucket "$B" --key canary/probe.txt --version-id "$V"
probe s3api delete-object-tagging --bucket "$B" --key canary/probe.txt
probe s3api delete-object-tagging --bucket "$B" --key canary/probe.txt --version-id "$V"
probe s3api put-object-tagging --bucket "$B" --key canary/probe.txt --tagging 'TagSet=[]'
probe s3api put-object-tagging --bucket "$B" --key canary/probe.txt --version-id "$V" --tagging 'TagSet=[]'
probe s3api put-object-acl --bucket "$B" --key canary/probe.txt --acl private
probe s3api put-object-retention --bucket "$B" --key canary/probe.txt --retention '{}'
probe s3api put-object-legal-hold --bucket "$B" --key canary/probe.txt --legal-hold Status=OFF
probe s3api abort-multipart-upload --bucket "$B" --key canary/probe.txt --upload-id does-not-exist
probe s3api restore-object --bucket "$B" --key canary/probe.txt --restore-request Days=1
# 6. The canary bucket
probe s3api delete-bucket-website --bucket "$CB"
probe s3api delete-bucket-policy --bucket "$CB"
probe s3api delete-bucket --bucket "$CB"
probe s3api put-bucket-versioning --bucket "$CB" --versioning-configuration Status=Suspended
probe s3api put-object-lock-configuration --bucket "$CB" --object-lock-configuration ObjectLockEnabled=Enabled
probe s3api put-bucket-cors --bucket "$CB" --cors-configuration '{"CORSRules":[{"AllowedMethods":["GET"],"AllowedOrigins":["https://example.com"]}]}'
probe s3api put-bucket-lifecycle-configuration --bucket "$CB" --lifecycle-configuration '{"Rules":[{"ID":"probe","Status":"Enabled","Filter":{"Prefix":"canary/"},"Expiration":{"Days":365}}]}'
probe s3api put-bucket-tagging --bucket "$CB" --tagging 'TagSet=[{Key=probe,Value=1}]'
probe s3api put-bucket-encryption --bucket "$CB" --server-side-encryption-configuration '{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"AES256"}}]}'
# 7. Outside the project
xprobe s3api put-object --bucket <a bucket in another project of yours> --key canary/probe.txt --body "$W/probe.txt"
probe s3api list-buckets
# The state bucket (your admin project): the deploy key may neither read, list nor write it
xprobe s3api get-object --bucket <state-bucket> --key site/terraform.tfstate "$W/state-probe.json"
xprobe s3api list-objects-v2 --bucket <state-bucket>
xprobe s3api put-object --bucket <state-bucket> --key canary/probe.txt --body "$W/probe.txt"
# PASS: every probe line "PASS 403", and every xprobe line "PASS 403" or "PASS NoSuchBucket"
# FAIL "FAIL allowed" or "INCONCLUSIVE": record probe, answer and reason in the hosting ADR, restore (8), and stop until it is explained
# 8. Own key again: restore and clean up
export AWS_ACCESS_KEY_ID="$SCW_ACCESS_KEY" AWS_SECRET_ACCESS_KEY="$SCW_SECRET_KEY"
aws s3api put-bucket-acl --bucket "$B" --access-control-policy "file://$W/saved-acl.json"
aws s3api list-object-versions --bucket "$B" --prefix canary/ --query '[Versions,DeleteMarkers][][].VersionId' --output text   # each ID: aws s3api delete-object --bucket "$B" --key canary/probe.txt --version-id <ID>
aws s3api delete-bucket --bucket "$CB"
# FAIL "BucketNotEmpty": delete its versions as above first
rm -r "$W"
scw iam api-key update "$SCW_ACCESS_KEY" default-project-id="$ADMIN_PROJECT"   # back to the admin project, where the state is
# The switch takes a moment to apply at Scaleway: wait until your own key lists the state bucket again before any tofu touches its lock
for i in $(seq 1 12); do aws s3api list-objects-v2 --bucket <state-bucket> --max-items 1 >/dev/null 2>&1 && break; sleep 10; done
aws s3api list-objects-v2 --bucket <state-bucket> --query 'Contents[].Key'   # PASS: "site/terraform.tfstate" and no "site/terraform.tfstate.tflock"
# FAIL "AccessDenied" or "NoSuchBucket": the switch has not applied yet; wait a minute and list again
# FAIL "site/terraform.tfstate.tflock" listed and no tofu runs (a plan run before the switch applied left it): the plan below stops at "Error acquiring the state lock"; its FAIL line clears the lock
# If the state bucket's put-object probe was allowed: aws s3api delete-object --bucket <state-bucket> --key canary/probe.txt
dev/tofu.sh -chdir=infra/site plan -detailed-exitcode   # PASS: exit 0 (website, versioning and policy unchanged)
# FAIL exit 2: dev/tofu.sh -chdir=infra/site apply
# FAIL "Error acquiring the state lock": unlock only when its "Who" is this machine's user@host, its "Created" the time of the earlier run, and no other dev/tofu.sh runs on this machine; then, with its "ID" and without -force (tofu asks to confirm): dev/tofu.sh -chdir=infra/site force-unlock <lock ID>   # PASS: "OpenTofu state has been successfully unlocked!", the listing above shows no .tflock, then plan again
# FAIL "Error releasing the state lock": the listing and force-unlock above, then plan again
```

## R8. Namecheap

Domain List, `shift2eu.com`, Advanced DNS. Both names point at the container ([ADR 0045](adr/0045-front-the-site-with-a-serverless-container-running-a-pinned-caddy.md));
the pipeline keeps no name of its own. Five blocks, in order; each needs R4's output lines in the shell
(`$CE`, `$E`, `$NS`, `$PL`).

**1. The records**, before any custom domain exists (Scaleway checks them when the domain is created).

```bash
# CNAME www -> $CE (the container_endpoint output, replacing a www CNAME to the pipeline); ALIAS @ -> $CE; CAA @ 0 issue "letsencrypt.org"; CAA @ 0 issuewild ";"; DNSSEC on; no URL redirect record for @
dig +short CNAME www.shift2eu.com     # PASS: "$CE." (the container_endpoint output)
diff <(dig +short A shift2eu.com | sort) <(dig +short A "$CE" | sort) && echo "PASS the apex resolves to the container"   # PASS: that line
# FAIL a difference: the ALIAS is not saved yet, or a lookup made before it is cached (the zone's negative TTL is 3601 s): wait, then again
dig +short CAA shift2eu.com           # PASS: exactly 0 issue "letsencrypt.org" and 0 issuewild ";"
dig +short DS shift2eu.com            # PASS: a DS record (DNSSEC on)
dig +dnssec A shift2eu.com @1.1.1.1 | grep -E 'flags:.* ad|RRSIG'   # PASS: a flags line with "ad" and an RRSIG line: the apex stays signed and validated behind the ALIAS
```

**2. The second apply: the custom domains.** Set `front_domains = true` in `infra/site/terraform.tfvars`,
then:

```bash
grep -c '^front_domains *= *true' infra/site/terraform.tfvars   # PASS: 1
dev/tofu.sh -chdir=infra/site plan -out=domains.tfplan      # PASS: "Plan: 2 to add, 0 to change, 0 to destroy." (www.shift2eu.com and shift2eu.com on the container)
# FAIL any change or destroy: stop, apply nothing, rm infra/site/domains.tfplan, and find out why (R4's no-changes check)
dev/tofu.sh -chdir=infra/site apply domains.tfplan          # PASS: "Apply complete! Resources: 2 added, 0 changed, 0 destroyed."
# FAIL an error after about 3 minutes naming the DNS record: block 1's checks again, wait out a cached lookup (up to an hour), rm infra/site/domains.tfplan and plan and apply again (only what is left is added)
rm infra/site/domains.tfplan
dev/tofu.sh -chdir=infra/site output -json front_domains    # PASS: ["shift2eu.com","www.shift2eu.com"]
# Both domains ready, with their certificates: wait up to 30 minutes, 60 tries 30 s apart
n=0; until [ "$(scw container domain list region="$REGION" -o json | jq '[.[] | select(.hostname == "www.shift2eu.com" or .hostname == "shift2eu.com") | select(.status == "ready")] | length')" = 2 ] || [ "$n" -ge 60 ]; do sleep 30; n=$((n + 1)); done
scw container domain list region="$REGION" -o json | jq -r '.[] | "\(.hostname) \(.status) \(.error_message // "")"'   # PASS: "www.shift2eu.com ready" and "shift2eu.com ready"
# FAIL "error": its error_message names the record or the CAA; fix it at Namecheap, then delete that domain in the console and run block 2 again from the plan
for h in www.shift2eu.com shift2eu.com; do echo | openssl s_client -connect "$h:443" -servername "$h" 2>/dev/null | openssl x509 -noout -subject -issuer -enddate; done   # PASS: each name's own subject, a Let's Encrypt issuer
# FAIL "unable to load certificate": the domain is not ready yet, or the CAA record refuses the issuer
```

**3. The front's answers** (ADR 0045's table), after warming the container: the first request after an
idle period starts it (`min_scale` 0).

```bash
curl -s -o /dev/null -w 'warm %{http_code} %{time_total}s\n' --max-time 60 "https://www.shift2eu.com/"   # PASS: a status and a time; record the time (the live check allows 30 s)
for u in http://www.shift2eu.com/ http://shift2eu.com/ https://shift2eu.com/ "http://$CE/" "https://$CE/" "https://shift2eu.com/a?b=1"; do printf '%s ' "$u"; curl -s -o /dev/null -w '%{http_code} %{redirect_url}\n' "$u"; done
# PASS: every line "308 https://www.shift2eu.com/" (the last "308 https://www.shift2eu.com/a?b=1"); never a 200, never a 301 or 302
curl -sS -D - -o /dev/null https://www.shift2eu.com/ | grep -i -E '^(HTTP|strict-transport-security|content-security-policy|x-content-type-options|referrer-policy|permissions-policy|server|x-envoy)'
# PASS: "HTTP/2 200" (or 1.1), exactly one of each of the five headers with infra/site/Caddyfile's values, "server: envoy" and x-envoy-upstream-service-time from the platform, nothing else of theirs
curl -sS -D - -o /dev/null https://shift2eu.com/ | grep -ic '^strict-transport-security'   # PASS: 0 (the apex's 308 carries no HSTS while preload is not pursued)
```

**4. The live probes ADR 0045 requires before the site goes live.** Record every answer in ADR 0045's
amendment, *Still to be settled live*.

```bash
# The spoofed and repeated X-Forwarded-Proto over plain HTTP: each must get the 308, never the site
curl -s -o /dev/null -w 'one https: %{http_code}\n' -H 'X-Forwarded-Proto: https' http://www.shift2eu.com/
curl -s -o /dev/null -w 'https+http: %{http_code}\n' -H 'X-Forwarded-Proto: https' -H 'X-Forwarded-Proto: http' http://www.shift2eu.com/
curl -s -o /dev/null -w 'http+https: %{http_code}\n' -H 'X-Forwarded-Proto: http' -H 'X-Forwarded-Proto: https' http://www.shift2eu.com/
curl -s -o /dev/null -w 'https+https: %{http_code}\n' -H 'X-Forwarded-Proto: https' -H 'X-Forwarded-Proto: https' http://www.shift2eu.com/
curl -s -o /dev/null -w 'https, https: %{http_code}\n' -H 'X-Forwarded-Proto: https, https' http://www.shift2eu.com/
# PASS: every line 308. "one https" 200: the platform passes a client's own header through; record it, the maintainer's to accept or not (ADR 0045)
# FAIL any other line 200: the rule does not fail closed; stop, roll back (block 5) and record it
# OPTIONS * through the platform
printf 'OPTIONS * HTTP/1.1\r\nHost: www.shift2eu.com\r\nConnection: close\r\n\r\n' | openssl s_client -quiet -connect www.shift2eu.com:443 -servername www.shift2eu.com 2>/dev/null | head -5
# PASS, record which: Go's empty 200 (Content-Length: 0, no header of the site's), or the platform's own answer (it does not forward OPTIONS *)
# FAIL a body or a header of the site's: stop and record it
```

The headers and the host as the container receives them, from a throwaway probe container in the same
namespace that echoes them (its own endpoint; the custom domains stay on `front`). `www`'s and the apex's
values follow from block 3: `www` over HTTPS is served only when the joined `X-Forwarded-Proto` is
exactly `https` and the host exactly `www.shift2eu.com`, and plain HTTP gets the 308 only when it is not.
Check the flags against `scw container container create --help` first.

```bash
IMG="$(sed -n 's/^ *caddy_image *= *"\(.*\)"$/\1/p' infra/site/container.tf)"; echo "$IMG"   # PASS: the pinned image, by digest
ECHO=':8080 {
	header Content-Type "text/plain; charset=utf-8"
	header X-Content-Type-Options nosniff
	respond "xfp=[{http.request.header.X-Forwarded-Proto}] host=[{http.request.hostport}] xfh=[{http.request.header.X-Forwarded-Host}] fwd=[{http.request.header.Forwarded}]" 200
}'
PR="$(scw container container create namespace-id="$NS" region="$REGION" name=header-probe registry-image="$IMG" port=8080 privacy=public min-scale=0 max-scale=1 cpu-limit=140 memory-limit-bytes=268000000 https-connections-only=false command.0=/bin/sh command.1=-c args.0='printf "%s" "$1" > /tmp/Caddyfile && exec caddy run --adapter caddyfile --config /tmp/Caddyfile' args.1=caddy args.2="$ECHO" -o json | jq -r '.id')"; echo "$PR"   # PASS: an ID
scw container container deploy "$PR" region="$REGION" >/dev/null; n=0; until scw container container get "$PR" region="$REGION" -o json | jq -e '.status == "ready"' >/dev/null || [ "$n" -ge 30 ]; do sleep 20; n=$((n + 1)); done
PE="$(scw container container get "$PR" region="$REGION" -o json | jq -r '.domain_name // .public_endpoint' | sed 's,^https://,,')"; echo "$PE"   # PASS: the probe's endpoint
for u in "http://$PE/" "https://$PE/"; do printf '%s ' "$u"; curl -s "$u"; echo; done
curl -s -H 'X-Forwarded-Proto: https' -H 'X-Forwarded-Host: evil.example' "http://$PE/"; echo
curl -s -H 'X-Forwarded-Proto: https' -H 'X-Forwarded-Proto: http' "http://$PE/"; echo
# PASS, record each: xfp=[http] over plain HTTP and xfp=[https] over HTTPS (or what the platform sends, and whether a client's value survives); host=[<the endpoint>] with no port and in lower case; xfh and fwd
# FAIL host with a port, another case or empty, or the original host only in xfh: www's exact comparison would never match (a loop); stop before R10 and record it, the Caddyfile follows the probe
# The probe reflects what it is sent (text/plain, nosniff): on any FAIL or stop in this block, the delete line below first, before anything else (an empty $PR in a fresh shell: scw container container list namespace-id="$NS" region="$REGION" name=header-probe -o json | jq -r '.[].id')
curl -sS -D - -o /dev/null "https://$PE/" | grep -i -E '^(content-type|x-content-type-options):'   # PASS: "text/plain; charset=utf-8" and "nosniff"
scw container container delete "$PR" region="$REGION"; scw container container list namespace-id="$NS" region="$REGION" -o json | jq -r '.[].name'   # PASS: "front" alone
```

**5. Rollback**, if block 2, 3 or 4 fails and the cause cannot be fixed before R10: the site goes back to
the pipeline's own `www` as milestone 3 had it.

```bash
# First, no probe container left behind (block 4's or R4's)
scw container container list namespace-id="$NS" region="$REGION" -o json | jq -r '.[].name'   # PASS: "front" alone
# FAIL header-probe or digest-probe listed: scw container container delete <its ID> region="$REGION" (scw container container list namespace-id="$NS" region="$REGION" name=<its name> -o json | jq -r '.[].id'), then this line again
# Namecheap: the www CNAME back to $E (the edge_endpoint output); delete the ALIAS at @
# In terraform.tfvars: front_domains = false (the container's custom domains go) and pipeline_www = true (the TLS stage and fqdns = ["www.shift2eu.com"] come back); then R4, with no pull request
grep -E '^(front_domains|pipeline_www)[[:space:]]*=' infra/site/terraform.tfvars   # PASS: "front_domains = false" and "pipeline_www = true"
dig +short CNAME www.shift2eu.com     # PASS: "$E."
# The re-added TLS stage asks for its certificate once, at its creation; if that came before the CNAME resolved, recreate it as below
n=0; until echo | openssl s_client -connect www.shift2eu.com:443 -servername www.shift2eu.com 2>/dev/null | openssl x509 -noout -issuer 2>/dev/null | grep -q Encrypt || [ "$n" -ge 30 ]; do sleep 30; n=$((n + 1)); done
echo | openssl s_client -connect www.shift2eu.com:443 -servername www.shift2eu.com 2>/dev/null | openssl x509 -noout -issuer -enddate   # PASS: a Let's Encrypt issuer
# FAIL "unable to load certificate": check the CAA record, then the TLS stage
PL="$(dev/tofu.sh -chdir=infra/site output -raw pipeline_id)"; echo "$PL"   # PASS: the pipeline ID, not empty
scw edge-services tls-stage list pipeline-id="$PL" -o json | jq '.[] | {status, certificate_expires_at}'   # PASS: a date in certificate_expires_at
# FAIL certificate_expires_at: null after the wait: no certificate was issued (R4 re-created the stage before the CNAME pointed back, and Scaleway asks only once); recreate the TLS stage below and re-run the checks
dev/tofu.sh -chdir=infra/site plan -replace='scaleway_edge_services_tls_stage.site[0]' -out=tls.tfplan   # PASS: "Plan: 1 to add, 1 to change, 1 to destroy."
dev/tofu.sh -chdir=infra/site show tls.tfplan | less      # PASS: the TLS stage "must be replaced" ("create replacement and then destroy") and the DNS stage's tls_stage_id changes in place, nothing else
# FAIL the plan replaces, destroys or changes any other resource: stop, apply nothing, rm infra/site/tls.tfplan, and find out why (R4's no-changes check) before the recreate
dev/tofu.sh -chdir=infra/site apply tls.tfplan            # PASS: "Apply complete! Resources: 1 added, 1 changed, 1 destroyed."
# FAIL the apply stopped part-way (an error after the new stage was created): fix the error it printed, rm infra/site/tls.tfplan, then re-plan with no replace target, as the new stage is already in the state: dev/tofu.sh -chdir=infra/site plan -out=tls.tfplan (only what is left: the deposed old stage's destroy and/or the DNS stage's tls_stage_id update), review it with the show line (PASS here: 0 to add, at most 1 to change and 1 to destroy: only the deposed old stage and the DNS stage's tls_stage_id, nothing else), apply it, then continue with the rm line below; never re-apply the old tls.tfplan
# FAIL the apply failed before the new stage was created (it reports 0 added, and scw edge-services tls-stage list still shows one stage): nothing changed, and a plain plan shows no changes, so the stage would never be recreated: fix the error it printed, rm infra/site/tls.tfplan, run the replace plan line above again, review it with the show line (same PASS as above), then apply it
rm infra/site/tls.tfplan
# Then the wait loop, the certificate check and the TLS stage check again
curl -s -o /dev/null -w '%{http_code} %{redirect_url}\n' http://www.shift2eu.com/   # record it: the pipeline's 302 (ADR 0045's Context): a no-go for R10 until the front is back
# After a rollback, R10 waits: the pipeline alone meets neither the redirect nor the HSTS go/no-go (ADR 0045)
# Ending the rollback is pipeline_www = false and R4 again: the stage this rollback created carries create_before_destroy in its state, so that removal updates the DNS stage first; R4's state check confirms it before the apply (issue #595)
```

The release-tag skill's and the live check's DNS, certificate, redirect and header checks verify blocks
1 to 3 afterwards.

## R9. The GitHub site environment

With your admin token. The `release-tag` skill's step 2 checks it at every release: the branch policy,
the tag policy, both secrets, both variables, and neither key at repository level.

```bash
gh api -X PUT repos/<owner>/shift2eu/environments/site -F 'deployment_branch_policy[protected_branches]=false' -F 'deployment_branch_policy[custom_branch_policies]=true'
gh api repos/<owner>/shift2eu/environments/site --jq '.deployment_branch_policy'   # PASS: {"custom_branch_policies":true,"protected_branches":false}
gh api -X POST repos/<owner>/shift2eu/environments/site/deployment-branch-policies -f name='v*.*.*' -f type=tag
gh api repos/<owner>/shift2eu/environments/site/deployment-branch-policies --jq '.branch_policies[] | "\(.type) \(.name)"'   # PASS: exactly "tag v*.*.*"
gh secret set SCW_ACCESS_KEY --env site -R <owner>/shift2eu   # paste the deploy key's access key (R6)
gh secret set SCW_SECRET_KEY --env site -R <owner>/shift2eu   # paste its secret key
gh secret list --env site -R <owner>/shift2eu    # PASS: exactly SCW_ACCESS_KEY and SCW_SECRET_KEY
gh secret list -R <owner>/shift2eu               # PASS: neither name at repository level
# The bucket and region the deploy job's "Deploy the site" step passes as -bucket and -region: environment variables, not secrets
gh variable set SITE_BUCKET --env site --body "$B" -R <owner>/shift2eu        # tofu output's bucket_name (R4)
gh variable set SITE_REGION --env site --body "$REGION" -R <owner>/shift2eu   # R0's region (tofu output has no region)
gh variable list --env site -R <owner>/shift2eu  # PASS: exactly SITE_BUCKET and SITE_REGION, with the values of $B and $REGION
# Who may create a v* tag, which the environment above deploys: read only, rulesets are not this runbook's to change
for id in $(gh api repos/<owner>/shift2eu/rulesets --jq '.[] | select(.target == "tag") | .id'); do
  gh api "repos/<owner>/shift2eu/rulesets/$id" --jq '"\(.name): \(.enforcement) include=\(.conditions.ref_name.include | join(",")) rules=\([.rules[].type] | join(",")) bypass=\([.bypass_actors[]? | "\(.actor_type):\(.actor_id)"] | join(","))"'
done
# The release-tag skill's step 2 rule, over the "active" lines whose include covers v* tags (refs/tags/v*, refs/tags/v*.*.* or ~ALL) and that exclude none:
# PASS: together their rules hold "creation", "update" and "deletion"
# PASS: the creation-only ruleset ("Release tag creation") bypass is exactly Integration:<release-tag App's ID, release-tag step 2's table> and RepositoryRole:5
# PASS: every ruleset holding "update" or "deletion" ("Release tags": update, deletion, non_fast_forward) has bypass RepositoryRole:5 alone (or OrganizationAdmin)
# FAIL the release-please App among the tag bypass actors (Integration:5729776), another App or actor, an App beside update or deletion, or a missing rule:
# anyone so allowed could create or move a v* tag that the site environment deploys, and the release-tag skill's step 2 stops every release; record it and fix the rulesets before R10
# The next release, SITE_DEPLOY_ENABLED still unset: the release-tag skill's step 7 reports the deploy skipped
gh run view <that release's release.yml run ID> -R <owner>/shift2eu --json jobs --jq '.jobs[] | select(.name == "Site programs" or .name == "Deploy the site" or .name == "Check the live site" or .name == "Site deploy skipped") | .name + " " + .conclusion'
# PASS: "Site programs success", "Deploy the site skipped", "Check the live site skipped", "Site deploy skipped success"
```

## R10. Turn the deploy on

Milestone 4, last; the release's live check reads the records at its tag. The deploy runs with
`-list-bucket` off and the live check with `-plain-http-www` at its default, `redirect`, until milestone 4
sets them (R7's ListBucket answer, R8's plain-HTTP answer).

```bash
# The HSTS and preload records PR merged, its squash commit with "Release-As: <version>", and release-please's release PR for it merged
git fetch --tags && git tag --contains "$(git log -1 --format=%H -- site/records/hsts.json)" 'v*'   # PASS: the release tag <version>
gh variable set SITE_DEPLOY_ENABLED --body true -R <owner>/shift2eu
gh variable list -R <owner>/shift2eu             # PASS: SITE_DEPLOY_ENABLED true
gh api repos/<owner>/shift2eu/actions/variables/SITE_DEPLOY_ENABLED --jq .value   # PASS: true (what the release-tag skill's step 2 reads; from now on any gap in R9 stops a release)
# The next release that is the highest release tag on main: the release-tag skill's step 7 reports the deploy and its live check
gh run view <that release's release.yml run ID> -R <owner>/shift2eu --json jobs --jq '.jobs[] | select(.name == "Site programs" or .name == "Deploy the site" or .name == "Check the live site" or .name == "Site deploy skipped") | .name + " " + .conclusion'
# PASS: "Site programs success", "Deploy the site success", "Check the live site success", "Site deploy skipped skipped"
# FAIL "Deploy the site failure" or "Check the live site failure": gh run view <run ID> -R <owner>/shift2eu --log-failed, then R12
# Then R14: the site now serves visitors, so every product has logged and each region's Scaleway logs source exists
```

## R11. Rotate the deploy key

Before its expiry; then R7 again.

```bash
# R6 for the new key, then R9's two gh secret set lines with it
scw iam api-key delete <old access key>
scw iam api-key list bearer-id="$A"   # PASS: one key, the new one
```

## R12. Recovery

PRD #297 Hosting decision 5, ADR 0042 (Recovery). Either redeploy the last good release, within GitHub's
30-day re-run window, or roll the objects back with your own key; when the bad write did not come from a
release run (a leaked key), also rotate the key and clean the bucket against every release's manifest;
then, in every case, purge the cache.

```bash
# Either redeploy the last good release: the release.yml run of the highest release tag on main
git fetch --tags && git tag -l 'v*.*.*' --merged origin/main | sed -nE '/^v[0-9]+\.[0-9]+\.[0-9]+$/p' | sort -V | tail -n 1   # PASS: the tag, vX.Y.Z
gh run list -R <owner>/shift2eu --workflow release.yml --branch <that tag> --json databaseId,createdAt,conclusion   # its run ID and when it started
# GitHub re-runs a run's jobs only within 30 days of its start, and the deploy and live check programs' artifacts are kept exactly that long
# FAIL the run started more than 30 days ago: nothing of it can be re-run; take the version rollback below, or cut a new release whose squash commit carries "Release-As: <next version>"
gh run view <run ID> -R <owner>/shift2eu --json jobs --jq '.jobs[] | select(.name == "Deploy the site") | .databaseId'   # the deploy job's ID
gh run rerun <run ID> -R <owner>/shift2eu --job <deploy job ID>     # re-runs "Deploy the site", then "Check the live site"; never re-run "Site bundle"
gh run watch <run ID> -R <owner>/shift2eu
gh run view <run ID> -R <owner>/shift2eu --json jobs --jq '.jobs[] | select(.name == "Deploy the site" or .name == "Check the live site") | .name + " " + .conclusion'
# PASS: "Deploy the site success" and "Check the live site success"
# FAIL: gh run view <run ID> -R <owner>/shift2eu --log-failed; until it is explained, the version rollback below
```

```bash
# Or roll back with your own key (versioning on; also after the 30-day window): every object a deploy writes but
# the content-addressed assets, which keep their names and are never deleted: the pages $GOOD's manifest lists (five from
# issue #528 on, the four without about.html before it; ADR 0042, amendment for issue #528), LICENSE, NOTICE and manifest.json
scw iam api-key update "$SCW_ACCESS_KEY" default-project-id="$P"   # its default project the site's (R5)
GOOD='<the last good release, vX.Y.Z>'; T='<a UTC time after its deploy and before the bad write, such as 2026-12-01T10:00:00Z>'
W="$(mktemp -d)"   # scratch files outside the checkout
gh release download "$GOOD" -R <owner>/shift2eu --pattern "shift2eu-site_$GOOD.tar.gz" --dir "$W"
gh attestation verify "$W/shift2eu-site_$GOOD.tar.gz" --repo <owner>/shift2eu \
  --signer-workflow <owner>/shift2eu/.github/workflows/release.yml \
  --source-ref "refs/tags/$GOOD" --deny-self-hosted-runners \
  && tar -xzOf "$W/shift2eu-site_$GOOD.tar.gz" manifest.json > "$W/good-manifest.json"   # PASS: "Verification succeeded"
# FAIL verification: no manifest is written, the copy below is skipped and every check line below FAILs: stop, the bundle is not to be trusted
jq -r .version "$W/good-manifest.json"   # PASS: the value of $GOOD
# Each key's newest version written before $T, copied over the current one, only after a verified manifest
test -s "$W/good-manifest.json" || echo "FAIL no verified manifest of $GOOD: the copy is skipped"
pages="$(jq -r '.files[].path | select(test("^(about|index|inventory|privacy|verify)\\.html$"))' "$W/good-manifest.json")"; echo $pages   # PASS: about.html index.html inventory.html privacy.html verify.html (the four without about.html before issue #528)
test -s "$W/good-manifest.json" && for k in $pages LICENSE NOTICE manifest.json; do
  # LastModified prints as +00:00 or Z, with or without milliseconds: both sides compared as epoch seconds
  v="$(aws s3api list-object-versions --bucket "$B" --prefix "$k" --output json | jq -r --arg k "$k" --arg t "$T" 'def s: sub("\\.[0-9]+"; "") | sub("\\+00:00$"; "Z") | fromdateiso8601; [.Versions[]? | select(.Key == $k and (.LastModified | s) < ($t | s))] | max_by(.LastModified | s) | .VersionId // empty')"
  if [ -n "$v" ] && aws s3api copy-object --bucket "$B" --key "$k" --copy-source "$B/$k?versionId=$v" --query VersionId --output text; then echo "PASS $k restored from $v"; else echo "FAIL $k: no version before $T, or the copy failed"; fi
done   # PASS: eight "PASS ... restored" lines (seven before issue #528), each after a new version ID
# Each restored object against the good release's manifest
for k in $pages LICENSE NOTICE; do
  want="$(jq -r --arg k "$k" '.files[] | select(.path == $k) | .sha256' "$W/good-manifest.json")"
  got="$(aws s3 cp "s3://$B/$k" - | shasum -a 256 | cut -d' ' -f1)"
  [ -n "$want" ] && [ "$got" = "$want" ] && echo "PASS $k is $GOOD's" || echo "FAIL $k: $got, $GOOD's manifest says $want"
done   # PASS: seven "PASS ... is <GOOD>'s" lines (six before issue #528)
aws s3 cp "s3://$B/manifest.json" - | cmp -s - "$W/good-manifest.json" && echo "PASS manifest.json is $GOOD's" || echo "FAIL manifest.json is not $GOOD's"
# $GOOD before issue #528: the newer release's about.html has no earlier version and is still served (ADR 0042, amendment
# for issue #528). Only when it should not stay until the fixing release, remove it (a delete marker), then purge the cache
test -s "$W/good-manifest.json" && { printf '%s\n' $pages | grep -qxF about.html || echo "about.html stays served: aws s3api delete-object --bucket $B --key about.html to remove it"; }
# FAIL a line: set another $T and run this whole block again (the lines below have removed $W)
rm -r "$W"
scw iam api-key update "$SCW_ACCESS_KEY" default-project-id="$ADMIN_PROJECT"   # back to the admin project
# The live checks fail until the expected version is live again; a rollback is followed by a fixing release,
# whose PR's squash commit carries "Release-As: <next version>" when it would cut no release by itself (as R10's records PR)
```

```bash
# Also when the bad write did not come from a release run (no release.yml run wrote it: a leaked deploy key).
# The two blocks above restore only the fixed keys (the pages, LICENSE, NOTICE and manifest.json): an overwritten content-addressed asset and objects at new paths
# (a look-alike page, an sw.js) would stay served. First R11: rotate the deploy key, and its old key deleted
scw iam api-key list bearer-id="$A"   # PASS: one key, the new one (R11)
scw iam api-key update "$SCW_ACCESS_KEY" default-project-id="$P"   # own key, its default project the site's (R5)
# Five blocks, in order: run each only after the one before it printed its last PASS line. Only the third and the fourth write to the bucket
GOOD='<the last good release, vX.Y.Z>'; W="$(mktemp -d)"; echo "$W"   # scratch files outside the checkout
# Every release's site bundle, attested, and its manifest: the keys a release has ever written.
# A release with no bundle asset is skipped; a bundle that cannot be listed, downloaded, verified or read is a FAIL, never skipped:
# its keys would drop out of the known keys, and the third block would delete them
# Drafts excluded: GoReleaser writes every release as a draft first, and a draft is never deployed (site-deploy needs publish)
: > "$W/failed.txt"
gh release list -R <owner>/shift2eu --exclude-drafts -L 1000 --json tagName --jq '.[].tagName' > "$W/releases.txt" || echo "the release list" >> "$W/failed.txt"
n=$(( $(wc -l < "$W/releases.txt") )); [ "$n" -lt 1000 ] || echo "the release list: $n, at its limit of 1000" >> "$W/failed.txt"; echo "$n"   # PASS: the number of releases, under 1000
for r in $(cat "$W/releases.txt"); do
  if ! gh release view "$r" -R <owner>/shift2eu --json assets --jq '.assets[].name' > "$W/assets.txt"; then
    echo "FAIL $r: its assets could not be listed"; echo "$r" >> "$W/failed.txt"; continue
  fi
  grep -qxF "shift2eu-site_$r.tar.gz" "$W/assets.txt" || { echo "SKIP $r: no site bundle asset (a release before the bundle)"; continue; }
  if gh release download "$r" -R <owner>/shift2eu --pattern "shift2eu-site_$r.tar.gz" --dir "$W" \
    && gh attestation verify "$W/shift2eu-site_$r.tar.gz" --repo <owner>/shift2eu \
      --signer-workflow <owner>/shift2eu/.github/workflows/release.yml \
      --source-ref "refs/tags/$r" --deny-self-hosted-runners \
    && tar -xzOf "$W/shift2eu-site_$r.tar.gz" manifest.json > "$W/manifest.tmp" \
    && test "$(jq -r .version "$W/manifest.tmp")" = "$r" \
    && mv "$W/manifest.tmp" "$W/manifest-$r.json"; then
    echo "PASS $r's manifest, its bundle attested"
  else
    echo "FAIL $r: it has a bundle asset that could not be downloaded, verified or read"; echo "$r" >> "$W/failed.txt"
  fi
done   # each "PASS" after gh's "Verification succeeded"
test ! -s "$W/failed.txt" && test "$(( $(wc -l < "$W/releases.txt") ))" -lt 1000 && test -s "$W/manifest-$GOOD.json" \
  && echo "PASS every site bundle attested, $GOOD's among them" || { echo "FAIL: stop here, and run this block again"; cat "$W/failed.txt"; }
```

```bash
# Second: the keys every release has written, and the bucket's current keys, with your own key. Kept as JSON: a key may hold any byte, a newline too
jq -s '[.[].files[].path] + ["manifest.json"] | unique' "$W"/manifest-v*.json > "$W/known-keys.json" && jq length "$W/known-keys.json"   # PASS: a count
aws s3api list-objects-v2 --bucket "$B" --output json > "$W/live.json" && jq '[.Contents[]?.Key] | length' "$W/live.json"   # PASS: a count
jq --slurpfile known "$W/known-keys.json" '[.Contents[]?.Key] | unique - $known[0]' "$W/live.json" > "$W/unknown-keys.json" && jq . "$W/unknown-keys.json" && echo PASS   # record them: the leaked key's new paths
```

```bash
# Third, the deletion: every key that is in no release's manifest (versioning on: a delete marker, the written version kept for the record).
# Guarded: it deletes nothing unless the first block passed and the known keys hold $GOOD's fixed ones
del=""
if ! { test ! -s "$W/failed.txt" && test -s "$W/manifest-$GOOD.json" && jq -e 'index("manifest.json") and index("index.html")' "$W/known-keys.json" >/dev/null; }; then
  echo "FAIL: the first block did not pass; nothing deleted"
elif jq -e 'length == 0' "$W/unknown-keys.json" >/dev/null; then
  echo "PASS: no key to delete"
elif jq -e 'length > 1000' "$W/unknown-keys.json" >/dev/null; then
  echo "FAIL: more than 1000 keys, more than one delete-objects call takes; nothing deleted"
else
  jq '{Objects: [.[] | {Key: .}], Quiet: false}' "$W/unknown-keys.json" > "$W/delete.json"
  if aws s3api delete-objects --bucket "$B" --delete "file://$W/delete.json" --output json > "$W/deleted.json" \
    && jq -e '(.Errors // []) | length == 0' "$W/deleted.json" >/dev/null; then
    echo "PASS deleted $(jq '.Deleted | length' "$W/deleted.json") keys"
  else
    echo "FAIL: some keys not deleted"; jq '.Errors' "$W/deleted.json"; del=failed
  fi
fi   # PASS: as many keys deleted as the second block listed
# FAIL MissingContentMD5/InvalidRequest (delete-objects refused): the keys one by one, only after that FAIL, inside the guard
[ "$del" = failed ] && jq -r '.[]' "$W/unknown-keys.json" | while IFS= read -r k; do aws s3api delete-object --bucket "$B" --key "$k" >/dev/null && echo "PASS deleted $k" || echo "FAIL $k not deleted"; done
aws s3api list-objects-v2 --bucket "$B" --output json | jq -c --slurpfile known "$W/known-keys.json" '[.Contents[]?.Key] | unique - $known[0]'   # PASS: []
# FAIL keys listed: the second and third blocks again
```

```bash
# Fourth: every key a release has written, against the digests its releases' manifests list and the headers the deploy sets
# (ci/sitedeploy/internal/deploy/bucket.go). $GOOD's keys must be $GOOD's; a key only other releases list may be any of theirs.
# module.* objects are stored gzip-compressed, with Content-Encoding: gzip: such a version is hashed through gunzip -c.
# A version that matches is current: PASS; else copied over the current one (copy-object keeps its headers).
# No version matches: $GOOD's key a FAIL, another release's key deleted (a delete marker).
# A version that cannot be downloaded: the key a FAIL, neither restored nor deleted
H='def want: "public, max-age=300" as $s | "public, max-age=31536000, immutable" as $i
  | if . == "manifest.json" then ["application/json", $s, null]
    elif test("^(about|index|inventory|privacy|verify)\\.html$") then ["text/html; charset=utf-8", $s, null]
    elif . == "LICENSE" or . == "NOTICE" then ["text/plain; charset=utf-8", $s, null]
    elif test("^style\\.[0-9a-f]{16}\\.css$") then ["text/css; charset=utf-8", $i, null]
    elif test("^(page|wasm_exec)\\.[0-9a-f]{16}\\.js$") then ["text/javascript; charset=utf-8", $i, null]
    elif test("^module\\.[0-9a-f]{16}\\.js$") then ["text/javascript; charset=utf-8", $i, "gzip"]
    else null end;
  ($k | want) as $w | $w != null and .ContentType == $w[0] and .CacheControl == $w[1] and .ContentEncoding == $w[2]
  and .WebsiteRedirectLocation == null and .ContentDisposition == null and .ContentLanguage == null and .Expires == null
  and ((.Metadata // {}) | length) == 0'
: > "$W/failed-keys.txt"; ok=no   # guarded as the third block: no key is checked, restored or deleted unless the first block passed
test ! -s "$W/failed.txt" && test -s "$W/manifest-$GOOD.json" && jq -e 'index("manifest.json") and index("index.html")' "$W/known-keys.json" >/dev/null && ok=yes
for k in $([ "$ok" = yes ] && jq -r '.[]' "$W/known-keys.json"); do
  ours=yes
  if [ "$k" = manifest.json ]; then want="$(shasum -a 256 < "$W/manifest-$GOOD.json" | cut -d' ' -f1)"
  else want="$(jq -r --arg k "$k" '.files[] | select(.path == $k) | .sha256' "$W/manifest-$GOOD.json")"; fi
  [ -n "$want" ] || { ours=no; want="$(jq -r --arg k "$k" '.files[] | select(.path == $k) | .sha256' "$W"/manifest-v*.json | sort -u)"; }
  if ! aws s3api list-object-versions --bucket "$B" --prefix "$k" --output json > "$W/versions.json"; then
    echo "FAIL $k: its versions could not be listed"; echo "$k" >> "$W/failed-keys.txt"; continue
  fi
  cur="$(jq -r --arg k "$k" '.Versions[]? | select(.Key == $k and .IsLatest) | .VersionId' "$W/versions.json")"
  v=""; dl=""
  for c in $(jq -r --arg k "$k" '.Versions[]? | select(.Key == $k) | .VersionId' "$W/versions.json"); do
    aws s3api get-object --bucket "$B" --key "$k" --version-id "$c" "$W/candidate" --output json > "$W/head.json" || { dl="$c"; break; }
    jq -e --arg k "$k" "$H" "$W/head.json" >/dev/null || continue
    if [ "$(jq -r '.ContentEncoding // ""' "$W/head.json")" = gzip ]; then got="$(gunzip -c < "$W/candidate" | shasum -a 256 | cut -d' ' -f1)"
    else got="$(shasum -a 256 < "$W/candidate" | cut -d' ' -f1)"; fi
    printf '%s\n' "$want" | grep -qxF "$got" && { v="$c"; break; }
  done
  if [ -n "$dl" ]; then echo "FAIL $k: version $dl could not be downloaded; nothing restored or deleted"; echo "$k" >> "$W/failed-keys.txt"
  elif [ -n "$v" ] && [ "$v" = "$cur" ]; then echo "PASS $k is current and a release's"
  elif [ -n "$v" ]; then
    aws s3api copy-object --bucket "$B" --key "$k" --copy-source "$B/$k?versionId=$v" --query VersionId --output text \
      && echo "PASS $k restored from $v" || { echo "FAIL $k: the copy of $v failed"; echo "$k" >> "$W/failed-keys.txt"; }
  elif [ "$ours" = yes ]; then echo "FAIL $k: no version with $GOOD's digest and the deploy's headers"; echo "$k" >> "$W/failed-keys.txt"
  elif [ -z "$cur" ]; then echo "PASS $k: another release's key, not served"
  else
    aws s3api delete-object --bucket "$B" --key "$k" >/dev/null \
      && echo "PASS $k deleted: no version of another release's" || { echo "FAIL $k not deleted"; echo "$k" >> "$W/failed-keys.txt"; }
  fi
done
if [ "$ok" != yes ]; then echo "FAIL: the first block did not pass; nothing checked"
elif [ -s "$W/failed-keys.txt" ]; then echo "FAIL these keys:"; cat "$W/failed-keys.txt"
else echo "PASS every key a release's"; fi
# FAIL a key of $GOOD's with no matching version: redeploy $GOOD (the first block of R12, within 30 days), or cut a fixing release
# PASS: "PASS every key a release's"; then run this block once more: only "is current" and "not served" lines, and that last PASS line
```

```bash
# Fifth, after the fourth block's second run changed nothing: the manifest, and clean up
aws s3 cp "s3://$B/manifest.json" - | cmp -s - "$W/manifest-$GOOD.json" && echo "PASS manifest.json is $GOOD's" || echo "FAIL manifest.json is not $GOOD's: the version rollback above"
rm -r "$W"
scw iam api-key update "$SCW_ACCESS_KEY" default-project-id="$ADMIN_PROJECT"   # back to the admin project
# Then the purge below, and last the guide's live check against $GOOD: docs/guide/verify-releases.md, "Verify the site", with TAG="$GOOD"
# PASS: "Verification succeeded", "OK" for every file and "PASS: every file matches"; FAIL a "FAILED" line: these five blocks again, from the first
```

```bash
# Then, in every case, purge the cache
scw edge-services purge-request create pipeline-id="$PL" all=true   # PASS: a purge request
# FAIL "unknown command": purge in the console, the pipeline's Cache tab
# Browsers may keep a page from a poisoned cache for up to its max-age (300 seconds) after the purge
# A disabled schedule: re-enable the scheduled live check's workflow (milestone 4 adds it and names its file)
gh workflow enable <the scheduled live check's workflow> -R <owner>/shift2eu && gh workflow list -R <owner>/shift2eu   # PASS: it is "active"
```

## R13. Remove the site

The DNS records first, so no dangling CNAME or ALIAS can be taken over.

```bash
# Namecheap: delete the www CNAME and the ALIAS at @
dig +short CNAME www.shift2eu.com            # PASS: no output
dig +short A shift2eu.com                    # PASS: no output (a cached answer can last up to the record's TTL)
gh variable set SITE_DEPLOY_ENABLED --body false -R <owner>/shift2eu
scw iam api-key delete <deploy access key>
# Own key, default project the site's (R5): delete every object version and delete marker in "$B" (R7, step 8), then back to the admin project
scw iam api-key update "$SCW_ACCESS_KEY" default-project-id="$ADMIN_PROJECT"
# The switch takes a moment to apply at Scaleway: wait until your own key lists the state bucket again before the destroy touches its lock
for i in $(seq 1 12); do aws s3api list-objects-v2 --bucket <state-bucket> --max-items 1 >/dev/null 2>&1 && break; sleep 10; done
aws s3api list-objects-v2 --bucket <state-bucket> --query 'Contents[].Key'   # PASS: "site/terraform.tfstate" and no "site/terraform.tfstate.tflock"
# FAIL "AccessDenied" or "NoSuchBucket": the switch has not applied yet; wait a minute and list again
# FAIL "site/terraform.tfstate.tflock" listed and no tofu runs: R7 step 8's recovery (the plan's "Error acquiring the state lock" FAIL line), then list again
# The site environment's secrets and variables
gh secret delete SCW_ACCESS_KEY --env site -R <owner>/shift2eu && gh secret delete SCW_SECRET_KEY --env site -R <owner>/shift2eu
gh variable delete SITE_BUCKET --env site -R <owner>/shift2eu && gh variable delete SITE_REGION --env site -R <owner>/shift2eu
gh secret list --env site -R <owner>/shift2eu; gh variable list --env site -R <owner>/shift2eu   # PASS: no output
dev/tofu.sh -chdir=infra/site destroy        # PASS: "Destroy complete"
# FAIL "BucketNotEmpty": delete the remaining versions first
```

## R14. Log retention

After R10, and again after any change that puts one of the site's products in another region. Scaleway
keeps its products' logs (they can name a visitor's IP address and path: the container platform's logs
the visitor's address, Edge Services' logs the front's, and a visitor's only on a direct request to the
pipeline's endpoint) in one *Scaleway logs*
data source per region of the project, 7 days by default, created only once a product there has logged;
OpenTofu does not manage them (ADR 0039, *Amendment: log retention*). This sets each to 1 day, the
minimum, and checks the *Scaleway metrics* sources, the aggregate request counts the privacy page names
(no IP address, no user agent, no cookie, kept 31 days), at 31 days.

```bash
# Every region at once: the logs land in the region of the product that wrote them
scw cockpit data-source list project-id="$P" origin=scaleway region=all -o json | jq -r '.[] | [.region, .type, .retention_days, .id, .name] | @tsv'
# PASS: a "logs" line for $REGION (the bucket and the container) and one for Edge Services' region (fr-par, if it is not $REGION), and "metrics" lines
# FAIL no "logs" line, or none for one of those regions: that product has not logged yet; after the site has served a request through it, R14 again
# For each "logs" line whose retention_days is not 1. Console: Cockpit, project shift2eu-site, Data sources, Scaleway logs of that region, edit retention, 1 day, confirm. Or:
scw cockpit data-source update <id> retention-days=1 region=<region>   # PASS: prints the source with retention_days 1
# The check, every source at once
scw cockpit data-source list project-id="$P" origin=scaleway region=all -o json | jq -e 'type == "array" and ([.[] | select(.type == "logs")] | length > 0 and all(.retention_days == 1)) and ([.[] | select(.type == "metrics")] | all(.retention_days == 31))' >/dev/null && echo "PASS logs 1 day, metrics 31 days" || echo "FAIL a logs source above 1 day, a metrics source not at 31 days, no logs source, or an error"
# FAIL a logs source: the update above for it; a metrics source: set it back to 31 (its minimum), or change the privacy page's "kept 31 days" first; an error: run the list line alone and read it
```
