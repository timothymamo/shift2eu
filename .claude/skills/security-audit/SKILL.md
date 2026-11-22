---
name: security-audit
description: Security audit of shift2eu changes - cloud credential handling, read-only access to customer clouds, data leakage in reports, input handling, dependency and CI supply chain. Use when asked to audit a diff, branch, PR or the repository for security issues.
---

# Security audit

This tool reads other people's cloud estates. The two worst outcomes are **changing a customer's cloud**
and **leaking what is in it, or the credentials used to read it**. Audit with that in mind.

## Run the tools

Work in the worktree named in the task: run every non-git command from it (`cd <path> && ...`). Run git
as `git -C <path> ...`, never `cd <path> && git ...`. Write the worktree path out in full; do not store it
in a shell variable. After `git -C <path> fetch origin`:

- `govulncheck ./...` (reachable vulnerabilities in code and dependencies)
- `make lint`: gosec runs with the other enabled linters natively and for every release target, so a
  file only one platform builds is checked too; gosec's issues end in `(gosec)`
- `git -C <path> diff origin/main...HEAD -- go.mod go.sum` for every new or bumped dependency

Signal only what you started (CLAUDE.md rule 10): stop a run of your own only by the pid you kept, after checking it is still your command; never a process found by name, never a group.

## Checklist

**Cloud access is read-only**
- Source adapters call only `Describe*`, `List*`, `Get*` style APIs. Flag any create, update, delete or
  tag call.
- Documented IAM policies, Azure roles and GCP roles are read-only and minimal (`ReadOnlyAccess` or
  narrower, `Reader`, `roles/viewer` or narrower).

**Credentials**
- Credentials come only from the provider SDK's standard chain or explicit env vars; never from flags that
  end up in shell history; never written to disk, logs, errors, reports or test fixtures.
- No secrets in committed files: check the diff range for keys, tokens and real account identifiers.
- Code, tests and scripts never dump the process environment (`os.Environ()`, `env`, `printenv`) into
  logs, errors, reports or fixtures: the agents' API keys and tokens are in it.

**Data handling**
- Reports and logs leave out secrets, connection strings, tags that hold secrets, and user data.
- Real inventories and reports stay out of git (`.gitignore` covers `/inventories/` and `/reports/`).
- Fixtures are sanitised.

**Input handling**
- Untrusted input (config files, inventory JSON, provider API responses) is validated; no path traversal
  in output paths; no shell-outs built from input; HTML output escapes content.
- TLS verification is never disabled; every HTTP client has a timeout.

**Supply chain and CI**
- GitHub Actions are pinned by full commit SHA with a version comment; workflows declare least-privilege
  `permissions`; no `pull_request_target` that checks out PR code; checkouts use
  `persist-credentials: false`.
- New dependencies are maintained, compatibly licensed, and actually needed.
- Pinned versions Renovate cannot detect by itself (such as the `param:` versions in `dev/lima.yaml`)
  keep their `# renovate:` comment, so Renovate can patch them. A pin without one silently goes stale.
- For a Renovate PR: read the release notes between the old and new version for security fixes,
  breaking changes and new maintainers or ownership changes, and confirm a changed action SHA
  matches the tag in its comment.

## Report

For each finding: **severity** (critical / high / medium / low), `path:line`, the concrete attack or
failure scenario, and the fix. Include the `govulncheck` summary. Say explicitly when a category is clean.
