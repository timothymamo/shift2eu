# Security

How `shift2eu`'s security is checked beyond each change's review: a scheduled audit of the whole
repository and of the latest published release, how its findings are disclosed, how to verify a release
yourself, and where the dated audit reports are. To report a vulnerability, follow the
[security policy](../../SECURITY.md).

## The scheduled audit

Every change is reviewed as a diff: each unit's security audit, the AI reviewer
([ADR 0033](../adr/0033-review-pull-requests-with-an-ai-reviewer-that-only-comments-or-requests-changes.md)),
the maintainer's approval, and gosec, govulncheck and CodeQL in CI. The scheduled audit looks at what no
diff shows: the whole tree on `main` and its git history, and the **latest published release** (the CLI's
release archives, `checksums.txt`, its signed image, the image's attestations and SBOM), as an attacker
would. It is planned in PRD #175 ([`prds/175-scheduled-security-audit.md`](../../prds/175-scheduled-security-audit.md))
and recorded in [ADR 0040](../adr/0040-audit-the-repository-and-the-latest-release-on-a-schedule.md).

- **Cadence: monthly**, on the 3rd of each month at 04:17 UTC, once the schedule is enabled. The
  maintainer can also start a run by hand at any time, for example after a release. This page is the one
  user-facing page that states the cadence; every other document links here.
  [ADR 0040](../adr/0040-audit-the-repository-and-the-latest-release-on-a-schedule.md) records it, with
  the values derived from it.
- **Read-only.** A run scans the tree and its history, verifies the release's checksums, attestations and
  signatures, scans the image and binaries, and tests the released CLI with hostile input **locally**, in
  containers without network on GitHub's runner. It never changes code, never touches a cloud, and writes
  to GitHub only its report's pull request, its draft advisories and, later, `security` issues.
- **A model triages, a script decides.** The checks run with no model. A model, Claude, triages their
  results in a sandbox with no network for its commands; a model-free script then decides what is public
  and makes every write.
- **Not running yet.** The audit's workflow and its first run come in the PRD's later milestones. Until
  the first run, no report exists.

## How findings are disclosed

A report's pull request is public the moment it is opened, before the maintainer reviews it, so what is
public is decided in the run, from each finding's severity, confirmation and category alone:

| Finding | Where it goes |
|---|---|
| Critical, high or medium, confirmed or suspected | A draft security advisory, visible only to the repository's administrators (and anyone they add to the advisory) |
| Injection, authentication bypass, credential exposure or remote code execution, whatever the severity | A draft security advisory |
| Missing or unknown severity or category | A draft security advisory |
| A secret found in the git history | A draft security advisory naming the commit and the rule, never the value, for the maintainer to check |
| Confirmed low or informational, hardening included | The public report, then a `security` issue once the maintainer has merged that report |
| Suspected low or informational | The public report's "needs investigation" list |

The public report says only **whether** private findings exist ("handled privately", "not filed" when
advisory creation was refused, or "none"), never how many, so it does not announce an undisclosed flaw before its fix. That flag still tells a reader that
something exists; the maintainer accepts that. The audit never publishes an advisory or requests a CVE:
that is the maintainer's decision.

## Verify a release yourself

Every image is signed and carries build provenance and an SBOM, and every release archive carries build
provenance. [Verify a release](../guide/verify-releases.md) shows how to check them before you run
anything. A run checks the latest release the same way.

## The reports

Each run adds one report, in its own file, named `docs/security/audits/YYYY-MM-DD-<run_id>.md`: the run's
UTC date, then the GitHub Actions run ID. A second run on the same day adds a file of its own. Names start
with the date, so they sort by date, and the directory [`audits/`](audits/) lists every report in that
order (on GitHub, the directory view lists them by name, oldest first): that listing is the index, so a new report needs no edit to this page, and two reports' pull
requests never conflict. No report exists yet; the first comes with the audit's first run.

Every report follows [the report template](audits/TEMPLATE.md): a summary; its scope (commit, release tag,
tool versions, model); the public findings; whether private findings exist; the coverage of each check;
the changes since the last report, and any gap in the schedule; the public findings dismissed as false
positives; and the run's limits.
