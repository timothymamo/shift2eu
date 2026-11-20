# Run with Docker

The `shift2eu` image runs `assess` with no network, as your own user, on an inventory mounted
read-only. This page shows the commands and why each flag is there, and how credentials will reach the
container once live sources exist. [Install](install.md#the-images) says which image to pull or how to
build it.

The entrypoint is `shift2eu` and the working directory `/work`, so relative paths such as
`--inventory inventory.json` resolve there. Mount only the inventory file, read-only, and since
`assess` is offline, give the container no network:

```bash
docker run --rm --read-only --network none --user "$(id -u):$(id -g)" \
  --mount type=bind,src="$PWD/inventory.json",dst=/work/inventory.json,readonly \
  ghcr.io/timothymamo/shift2eu:v0.2.0 assess --inventory inventory.json --target scaleway

# The report describes your infrastructure: keep the file private.
(umask 077 && docker run --rm --read-only --network none --user "$(id -u):$(id -g)" \
  --mount type=bind,src="$PWD/inventory.json",dst=/work/inventory.json,readonly \
  ghcr.io/timothymamo/shift2eu:v0.2.0 assess --inventory inventory.json --target ovhcloud > report.md)
```

The image holds the binary alone, not the sample inventories: to try it without an inventory of your
own, mount one of the samples under `examples/`, from a release archive
([Install](install.md#the-release-archives)) or a clone of the repository, as [Try it](try-it.md) does.

## Mounts, users and reports

The image's own user is the non-root `nonroot` (uid 65532), which cannot read a file only you may read,
such as an inventory of mode `0600`. `--user "$(id -u):$(id -g)"` runs the container as you instead,
still not as root, so it reads your private files without their mode being loosened; every example here
passes it. `--mount type=bind` fails if the file is missing, where `-v` would quietly create a
root-owned directory in its place. The image works with `--read-only`, and the subshell keeps
`umask 077` from applying to the rest of your session.

With a rootless runtime (rootless Docker, or nerdctl in the project's Lima VM), container user 0 is you,
not root, and `--user "$(id -u):$(id -g)"` maps to a subordinate UID that cannot read your private files:
pass `--user 0:0` instead.

Redirect the report from standard output as above. To use `--output` instead, mount a directory you
own; running as your own user lets the container write there without the directory being opened up:

```bash
mkdir -p reports && chmod 700 reports
docker run --rm --read-only --network none --user "$(id -u):$(id -g)" \
  --mount type=bind,src="$PWD/inventory.json",dst=/work/inventory.json,readonly \
  --mount type=bind,src="$PWD/reports",dst=/out \
  ghcr.io/timothymamo/shift2eu:v0.2.0 assess --inventory inventory.json --target ovhcloud --output /out/report.md
```

Never make the report directory world-writable (`chmod 777`) to let the container's user in.

Live sources do not exist yet. When they do, cloud credentials reach the container the way the
providers' SDKs read them, through their default credential chain, at `docker run` time. Under
`--user`, the container has no home directory of its own (`HOME` is `/`), so set
`-e HOME=/home/nonroot` wherever the SDK looks for config under the home directory:

- Preferably, the provider's config directory mounted read-only, with short-lived credentials in it
  (AWS SSO, `aws sts`) rather than long-lived keys. Its files are usually mode `0600`, which `--user`
  lets the container read. The AWS SSO token cache must stay writable, and must still show the token
  you already have, so bind-mount just that directory writable on top of the read-only mount:

  ```bash
  docker run --rm --read-only --user "$(id -u):$(id -g)" -e HOME=/home/nonroot -e AWS_PROFILE \
    --mount type=bind,src="$HOME/.aws",dst=/home/nonroot/.aws,readonly \
    --mount type=bind,src="$HOME/.aws/sso/cache",dst=/home/nonroot/.aws/sso/cache \
    ghcr.io/timothymamo/shift2eu:v0.2.0 ...
  ```

  A `--tmpfs` there would hide the cached token and SSO would fail. Keep `--tmpfs` for cache paths
  the SDK creates from scratch.
- Or environment variables, passed by name (`-e AWS_PROFILE -e AWS_REGION`) or with `--env-file` (a
  file of mode `0600` kept outside the repository). Never write `-e NAME=value`: the value ends up in
  your shell history and the process list. Passing by name or by file avoids only that. Any variable
  the container gets, however it was passed, is readable with `docker inspect` by anyone with access
  to the Docker daemon for as long as the container exists; `--rm` shortens that window but does not
  close it. Prefer short-lived credentials, or the read-only config mount above, to secrets in
  variables.

**Never** bake credentials into an image (no `COPY` or `ENV` of them, no build args), and never print
or log them.
