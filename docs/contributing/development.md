# Development

What you need to build and test `shift2eu`, the make targets you use every day, and how a change
reaches `main`.

Requirements: Go (version in `go.mod`), [golangci-lint](https://golangci-lint.run) v2,
[govulncheck](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck),
[shellcheck](https://www.shellcheck.net) (the version `dev/lima.yaml` pins), GNU Make. All of them come
preinstalled in the project's Lima VM ([Working with agents](agents.md)). actionlint needs no install:
`tools/go.mod`, a module of its own, pins it as a tool, and `dev/actionlint.sh` builds and runs it with
`go tool -modfile=tools/go.mod actionlint`.

```bash
make help     # list targets
make check    # lint (incl. architecture rules) + tests + vulnerability scan
make build    # ./bin/shift2eu
```

## Contributing

1. Start from a PRD (`prds/`) or an issue.
2. Work in its own worktree, never on `main`: `make worktree BRANCH=feat/<issue>-<slug>` prints the
   path. The hooks in `.githooks/` refuse commits and pushes to `main` (`make vm-init`
   enables them in the VM, as `make git-hooks` there does).
3. Test first, then implement; keep `make check` green.
4. Conventional Commits, one PR per milestone.

## Shell scripts and workflows

`make lint` also lints what is not Go
([ADR 0022](../adr/0022-lint-shell-scripts-and-workflows-with-shellcheck-and-actionlint.md)), and CI's
`Lint` check runs the same tools in its `lint-shell` job:

- **shellcheck** checks every shell script git tracks: every `*.sh` file and every file whose first line
  runs `sh` or `bash`, such as the hooks under `.githooks/`. `dev/shellcheck.sh` builds the list from
  `git ls-files`, so a new script is checked without any edit. It runs with `--norc`, so no
  `.shellcheckrc` in a parent directory or your home changes what it reports, and with
  `--severity=style`, so it reports every finding, including SC2086 (word splitting).
- **actionlint** checks `.github/workflows/*.yml`: syntax, `${{ }}` expressions and contexts. It passes
  each `run:` script to shellcheck, also with `--norc`: `dev/actionlint.sh` hands it the path of the
  shellcheck on the `PATH`, as actionlint would otherwise skip that check silently without one.
- **Without shellcheck** both steps fail with `shellcheck is not installed: re-provision the VM or see
  docs/contributing/development.md`. In the VM, see
  [the dev environment](../dev-environment.md#containers-and-image-verification); elsewhere, install it
  as below.
- **Disable directives:** fix a finding rather than silence it. When a check cannot be satisfied,
  silence it on its own line with a code and a reason:
  `# shellcheck disable=SC2086 # FLAGS is a list and must split`. `test/lint` refuses a directive
  without a code of the form `SC` and four digits (`disable=all` included) or without the reason after
  a second `#`, in a shell script or in a workflow's `run:` script, and refuses a tracked
  `.shellcheckrc`, `shellcheckrc` or `.github/actionlint.y*ml`. `make lint` also refuses an untracked
  `.github/actionlint.y*ml` and ignores `SHELLCHECK_OPTS`.

To install the pinned shellcheck outside the VM, take the version and the digest for your platform from
`dev/lima.yaml` (`shellcheckVersion`, `shellcheckSha256Amd64` or `shellcheckSha256Arm64`). The pins
cover Linux only. On a Mac, `brew install shellcheck` gives the current release, which can report
findings that the pinned version does not. On Linux:

```bash
v="$(sed -n 's/^ *shellcheckVersion: *"\(.*\)"$/\1/p' dev/lima.yaml)"; a=x86_64   # aarch64 on arm64
curl -fsSLO "https://github.com/koalaman/shellcheck/releases/download/${v}/shellcheck-${v}.linux.${a}.tar.gz"
sha256sum "shellcheck-${v}.linux.${a}.tar.gz"   # must equal shellcheckSha256Amd64 (Arm64 on aarch64)
tar -xzf "shellcheck-${v}.linux.${a}.tar.gz" && install -m 0755 "shellcheck-${v}/shellcheck" ~/.local/bin/
```
