# Security policy

`shift2eu` reads other people's cloud estates, so a flaw in it matters beyond this repository. This page
says which versions get security fixes and how to report a vulnerability privately.

## Supported versions

Only the **latest minor release** (the newest `vX.Y` line, in its latest patch) gets security fixes. A
fix ships as a new release; older releases are not patched. The [releases page](https://github.com/timothymamo/shift2eu/releases)
lists them, and [Verify a release](docs/guide/verify-releases.md) shows how to check one before you run it.

## Reporting a vulnerability

Report it privately through GitHub's private vulnerability reporting: open the repository's
**Security** tab and choose **Report a vulnerability**
([direct link](https://github.com/timothymamo/shift2eu/security/advisories/new)). Please do not open a
public issue, pull request or discussion for it.

Include what you can: the affected version or commit, the steps or input that show the problem, and the
impact you see. Never include credentials or data from a real cloud account; sanitised examples are
enough.

## What happens next

`shift2eu` is maintained by one person, so this is a best-effort commitment, not a guarantee:

- an acknowledgement of your report within 7 days;
- a first assessment (whether it is a vulnerability, and how severe) within 30 days;
- updates in the private advisory as the fix progresses, and credit in the published advisory if you
  want it.

## Audit reports

Besides each change's review, the repository and its latest release are audited on a schedule. How that
works, how its findings are disclosed, and its dated reports are in
[Security](docs/security/README.md).
