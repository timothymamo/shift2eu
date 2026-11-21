# Security audit report template

The sections every security audit report holds, in this order. This file is the template, not a report:
a report is named `YYYY-MM-DD-<run_id>.md` in this directory, and a file of any other name, this one
included, is never read as one. The audit's publish script fills a copy of these sections from the run's
structured result; the schedule and the disclosure rules are in [Security](../README.md). In a report, the
title is `# Security audit report, YYYY-MM-DD` and the guidance under each heading below is replaced by the
run's content.

## Summary

One paragraph: what was audited, the number of public findings by severity, whether private findings
exist (the flag below, never a count), and anything public the maintainer should read first; never a
private finding, a hint at one, or its existence beyond the private-findings flag.

## Scope

- **Commit:** the audited `main` SHA.
- **Release:** the latest published release's tag, and the image digest checked.
- **Tool versions:** each check's tool and its version.
- **Model:** the model and its effort.
- **Run:** the run's URL and its UTC date.

## Findings

The public findings only. One row per finding:

| Fingerprint | Severity | Category | Status | Location | Scenario | Recommended fix |
|---|---|---|---|---|---|---|
| `fp-` and 12 lowercase hex characters | critical, high, medium, low or informational | the finding's category | new, open or fixed | `path:line` or the release artifact | the concrete attack or failure | the fix |

"No public findings." when there are none.

The status describes this run's view of a finding: `new` when the last report did not list its
fingerprint, `open` when the last report listed it and the audited commit still has it, and `fixed` when
the last report listed it and the audited commit no longer has it. Changes since the last audit, below,
describes the **last report's** findings in its own words: fixed, still open (the table's `open`) or
regressed (a fixed finding that is back, `open` again in the table).

### Needs investigation

Suspected low or informational findings, not confirmed by a PoC or a precise trace, in the same columns.
They become no issue. "None." when there are none.

## Private findings

Exactly one of these three lines, never a count or a detail:

- `private findings: handled privately`
- `private findings: not filed`
- `private findings: none`

## Coverage

One row per check, with exactly one of these values:

| Check | Coverage |
|---|---|
| the check | `run`, `findings` or `not run (why)` |

`findings` means the check produced a public finding; `run` means it ran with no public finding; the why
of `not run (why)` is one of the publish script's fixed reasons.

## Changes since the last audit

Each public finding of the previous report, by fingerprint, as fixed, still open or regressed; the
dependencies changed since; and any gap in the schedule, when the previous report is older than the
schedule allows ([Security](../README.md)). On the first report, "First report."

## False positives

Every public finding dismissed as a false positive, this run's and every earlier report's, carried
forward from the last report so the newest report holds them all. One row each:

| Fingerprint | Reason |
|---|---|
| `fp-` and 12 lowercase hex characters | one line: why it is no finding |

A later run skips a finding only when its fingerprint matches a row here exactly, and never one of
severity medium or higher or in a private category. A private finding's false positive is never listed
here; it stays with its advisory. "None." when there are none.

## Limits

Every check that did not run or did not finish, every step the model declined or was refused, and every
question the run could not answer, each with its reason. "None." when there are none.
