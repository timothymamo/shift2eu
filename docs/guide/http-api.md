# Run the HTTP API

`shift2eu-server` serves the same assessments over a small, versioned HTTP API, for a website or any
other client that cannot run the CLI ([ADR 0019](../adr/0019-serve-assessments-over-a-versioned-http-api-from-a-separate-binary.md)).
It is a second binary, so `shift2eu` itself still opens no network connection. It takes an inventory
document and target keys, never cloud credentials, and keeps them only in memory while it answers: it
stores nothing and logs no request content. Its answers are byte for byte what `shift2eu assess` prints
for the same inventory and targets.

| Route | Answer |
|---|---|
| `POST /v1/assessments` | The assessment of one target, or the comparison of several (`["all"]` for every target), as the JSON report, schema version 2 ([ADR 0018](../adr/0018-version-the-json-report-format-for-the-cli-and-the-api.md), [ADR 0030](../adr/0030-serve-the-version-2-report-on-v1.md)) or, with `Accept: text/markdown`, the Markdown report, with plans and monthly estimates unless the request turns them off |
| `GET /v1/targets` | Every target's key, name, snapshot source and retrieval date |
| `GET /healthz` | `200 {"status":"ok"}` while the process runs |
| `GET /readyz` | `200 {"status":"ready"}` once every catalog has loaded, `503` until then |

[`api/openapi.json`](../../api/openapi.json) (OpenAPI 3.1) describes every route, request, response, header and
error; it refers to the report's JSON Schemas in
[`internal/adapter/report/jsonreport/schema/`](../../internal/adapter/report/jsonreport/schema/) by relative
path. Every error is `{"error": {"code": "...", "message": "..."}}`, and no message echoes a value from
the request.

Outside the repository those relative paths do not resolve, so each release, from the first after
`v0.3.0`, also publishes the self-contained document as the asset `shift2eu-server_openapi.json`: the
same document with the report schemas bundled under `components.schemas` (`AssessmentReportV2`,
`ComparisonReportV2`). The latest release's, that of the highest version released, as for the
images' `latest` tag, is always at
<https://github.com/timothymamo/shift2eu/releases/latest/download/shift2eu-server_openapi.json>, and a
given release's at `https://github.com/timothymamo/shift2eu/releases/download/vX.Y.Z/shift2eu-server_openapi.json`.
It is listed in the release's `checksums.txt` and carries the same GitHub build provenance as the
archives, so it is verified like one, by its digest and with `gh attestation verify`
([Verify an archive](verify-releases.md#verify-an-archive)). The repository holds the same file as
[`api/openapi.bundled.json`](../../api/openapi.bundled.json), generated from `api/openapi.json` (`make
openapi`) and held to it by a test; `api/openapi.json` stays the source to edit.

## Get it

Each release, from the first after `v0.2.0`, publishes `ghcr.io/timothymamo/shift2eu-server` for
`linux/amd64` and `linux/arm64`, with the same tags, signature, provenance and SBOM as the CLI's image.
It is signed and attested by the same release workflow of the same repository, so only the image name
changes: the certificate identity, `--repo` and `--signer-workflow` stay the CLI's
([Verify the image](verify-releases.md#verify-the-image) explains each check). The release's `verify` job runs these
checks on every release too, on a fresh runner, with `--help` as the server image's smoke test; it runs
after the image is published, so it detects a bad release rather than prevents one:

```bash
TAG=<version>
DIGEST="$(docker buildx imagetools inspect "ghcr.io/timothymamo/shift2eu-server:$TAG" --format '{{.Manifest.Digest}}')"
IMAGE="ghcr.io/timothymamo/shift2eu-server@$DIGEST"

cosign verify "$IMAGE" \
  --certificate-identity "https://github.com/timothymamo/shift2eu/.github/workflows/release.yml@refs/tags/$TAG" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-github-workflow-trigger push

gh attestation verify "oci://$IMAGE" --repo timothymamo/shift2eu \
  --signer-workflow timothymamo/shift2eu/.github/workflows/release.yml \
  --source-ref "refs/tags/$TAG" --deny-self-hosted-runners
gh attestation verify "oci://$IMAGE" --repo timothymamo/shift2eu \
  --signer-workflow timothymamo/shift2eu/.github/workflows/release.yml \
  --source-ref "refs/tags/$TAG" --deny-self-hosted-runners \
  --predicate-type https://spdx.dev/Document/v2.3

docker run --rm "$IMAGE" --help   # prints the flags and exits 0
```

It is distroless and runs as `nonroot`, with the entrypoint `/shift2eu-server` and the default
arguments (`CMD`) `--listen 0.0.0.0:8080`. Arguments given to `docker run` replace those, so pass
`--listen 0.0.0.0:8080` again with any of your own. Each release also publishes the binary in
archives of its own, `shift2eu-server_<version>_<os>_<arch>` ([Install](install.md#the-release-archives)),
listed in the same `checksums.txt` as the CLI's and verified the same way
([Verify an archive](verify-releases.md#verify-an-archive)); that check detects a corrupted download,
not a tampered release, so run the signed image where you need the publisher verified. From a clone, `make server` builds `./bin/shift2eu-server` and
`make server-image` the image `shift2eu-server:<version>` (`docker build --target server .` without
the version stamp). Deploy it as [Behind a reverse proxy](reverse-proxy.md) describes.

## Example requests

An assessment request is a JSON object with the inventory document, version 1 or 2
([ADR 0006](../adr/0006-json-inventory-file-format-v1.md), [ADR 0025](../adr/0025-json-inventory-file-format-v2.md)),
and the target keys. It may also carry the same choices as the CLI's flags:

| Field | Type | Meaning |
|---|---|---|
| `inventory` | object | The inventory document, version 1 or 2. Required. |
| `targets` | array of strings | Target keys, or `["all"]`. Required. |
| `regions` | object | Optional. One region per selected target, `{"scaleway": "nl-ams"}`, as `--region scaleway=nl-ams`: only the offerings in that region count, a resource offered only elsewhere gets fit `none` ("not offered in `<region>`"), and the estimates use that region's prices. A target without one is assessed in all its regions, with estimates in its default region. |
| `estimate` | boolean | Optional, `true` when absent. `false` is `--no-estimate`: no plans, no estimates, no cost summary and no source cost in the report. |

They are validated as the CLI validates its flags, before the inventory is assessed: a region the target
does not list, or a key the request does not select, is `400` with the code `invalid_region` and a
message naming the target and its regions; an empty region is `400 invalid_request`. Nothing is
assessed. Either inventory version gets the version 2 report; a version 1 inventory's resources have
no size, so they show "not estimated: size unknown"
([Assess an inventory](assess.md#regions-and-estimates) explains the estimate). The path stays `/v1`:
the new fields are optional and the report's `schemaVersion` says which version a response is.

Started from a clone with a token, the server listens on its default loopback
address; the requests read the token from the same variable, which keeps it out of your shell history, and
hand curl the header on standard input (`-H @-`), which keeps it out of the process list.
With the repository's sanitised example inventory:

```bash
export SHIFT2EU_SERVER_TOKEN="$(openssl rand -hex 32)"
make server && ./bin/shift2eu-server &   # listening on 127.0.0.1:8080

jq '{inventory: ., targets: ["scaleway"]}' test/e2e/testdata/inventory.json > request.json

# The JSON report.
printf 'Authorization: Bearer %s\n' "$SHIFT2EU_SERVER_TOKEN" |
  curl -sS -X POST http://127.0.0.1:8080/v1/assessments -H @- \
    -H 'Content-Type: application/json' \
    --data-binary @request.json

# The JSON report in a chosen region, with the estimates off.
jq '{inventory: ., targets: ["scaleway"], regions: {scaleway: "nl-ams"}, estimate: false}' \
  test/e2e/testdata/inventory.json > request.json
printf 'Authorization: Bearer %s\n' "$SHIFT2EU_SERVER_TOKEN" |
  curl -sS -X POST http://127.0.0.1:8080/v1/assessments -H @- \
    -H 'Content-Type: application/json' \
    --data-binary @request.json

# The Markdown report, comparing every target.
jq '{inventory: ., targets: ["all"]}' test/e2e/testdata/inventory.json > request.json
printf 'Authorization: Bearer %s\n' "$SHIFT2EU_SERVER_TOKEN" |
  curl -sS -X POST http://127.0.0.1:8080/v1/assessments -H @- \
    -H 'Content-Type: application/json' -H 'Accept: text/markdown' \
    --data-binary @request.json

# The targets, and the health checks.
printf 'Authorization: Bearer %s\n' "$SHIFT2EU_SERVER_TOKEN" | curl -sS -H @- http://127.0.0.1:8080/v1/targets
curl -sS http://127.0.0.1:8080/healthz
curl -sS http://127.0.0.1:8080/readyz
```

Through [the proxy](reverse-proxy.md), the same requests go to `https://api.example.com/v1/...` with the user's own
credentials (`curl -u <user>`) in place of the token.
