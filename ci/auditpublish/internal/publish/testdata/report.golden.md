# Security audit report, 2026-12-03

## Summary

The audit of commit 0123456789abcdef0123456789abcdef01234567 and release v0.1.0 by the scheduled security audit. [Security](../README.md) describes the audit and its reports.

- Public findings: 3 (2 low, 1 informational)
- Listed under needs investigation: 1
- private findings: none

## Scope

- Commit: 0123456789abcdef0123456789abcdef01234567
- Release: v0.1.0, image ` sha256:1111111111111111111111111111111111111111111111111111111111111111 `
- Tool versions:
  - go ` 1.27.4 `
  - gitleaks ` v8.29.0 `
- Model: claude-fable-5-2, effort high
- Run: [run 1234567890](https://github.com/example/shift2eu/actions/runs/1234567890), 2026-12-03 (UTC)

## Findings

| Fingerprint | Severity | Category | Status | Location | Scenario | Recommended fix |
|---|---|---|---|---|---|---|
| fp-6d0905dfb6fd | low | hardening | new | ` internal/cli/run.go:12 ` | ` a scenario ` | ` a fix ` |
| fp-8f0937b67b7d | informational | supply chain | new | ` internal/cli/run.go:12 ` | ` a scenario ` | ` a fix ` |

### Needs investigation

| Fingerprint | Severity | Category | Status | Location | Scenario | Recommended fix |
|---|---|---|---|---|---|---|
| fp-a02635997e16 | low | denial of service | new | ` internal/cli/run.go:12 ` | ` a scenario ` | ` a fix ` |

## Private findings

private findings: none

## Coverage

| Check | Coverage |
|---|---|
| govulncheck (source) and osv-scanner | run |
| gitleaks on the full history | run |
| zizmor and actionlint | run |
| OpenSSF scorecard | run |
| OpenSSF scorecard checks that need admin read | run |
| Release checksums, attestations and signatures | run |
| Image pulls from GHCR | run |
| govulncheck on the release binaries | run |
| trivy on the image and its SBOM | run |
| The image's runtime settings | run |
| The CLI on hostile inventory files | run |
| Fuzzing of the committed `Fuzz*` targets | run |
| Supply-chain reads | run |
| The security-audit checklist on the whole tree | findings |
| What the agents may run | run |

## Changes since the last audit

First report.

Dependencies changed since the last audit:

- ` github.com/google/go-cmp v0.7.0 to v0.8.0 | go.sum updated `

Narrative: ` No dependency changed maintainer. | The lock file is unchanged. `

## False positives

None.

## Limits

- ` scorecard's admin checks need a read the job does not hold. | The test fixture is one. `
