// Package audit is the logic of the scheduled security audit's model step
// (ADR 0040's model-job amendment, issue #277, PRD #175 milestone 2): the six
// steps ci/auditrun takes, in order, each failing the run before the next.
//
//  1. CheckManagedSettings refuses a root holding a Claude Code managed-settings
//     path, checked with Lstat.
//  2. InstallClaude checks the kept tarball's sha512 against the pin's npm
//     integrity and only then extracts its one entry, package/claude, mode 0500,
//     into state/bin, every directory from the root down to it checked as no
//     symbolic link; a PAX global header is skipped.
//  3. CheckLayout checks the audit directory under /srv/audit as the ADR lays
//     it out, refusing a symbolic link in its directories, any write bit, any
//     entry but a directory, a regular file or an in-tree link under checkout
//     and checks, and a checkout .git that is no directory, that holds a
//     commondir or gitdir, or whose config or config.worktree is no regular
//     file or holds a key off that file's allow list or a line its strict
//     parser cannot place.
//  4. CheckBubblewrap starts `bwrap --unshare-all --die-with-parent --ro-bind /
//     / /bin/true` once.
//  5. Preflight has the model make PreflightPlan's calls - a control that reads
//     a nonce back, then the six probes, each call with the exact input its
//     Targets fill in - and Judge, a pure function of the plan, the nonce, the
//     token, the canary, the transcript and the effects an Observer makes
//     under a context detached from the run's, passes only when the control
//     passed, every probe was refused with the plan's tools and inputs in its
//     order, no call's input holds dangerouslyDisableSandbox with any value
//     but false, and neither secret is anywhere in the transcript. Probe 5's
//     two reads the wrapper never sees are shell redirections, `cat <
//     /proc/{pid}/envir?n` and `cat < /dev/fd/../../{pid}/environ` (issue
//     #277 round H), so the sandbox, not the wrapper's path rule, is what
//     must refuse them on every run. Judge decodes the message of assistant
//     and user elements only and skips every other element, whatever it
//     holds; a line it cannot read is refused with its 1-based number and a
//     class from a fixed list, matched on the line's first 4 KiB, never with
//     any of its bytes (issue #590). It walks each element's top-level object
//     once, token by token, and refuses one that names a key twice or names
//     type or message in another case, which encoding/json would read
//     case-insensitively, keeping the last.
//  6. Audit checks that the work area again holds only its empty tmp/, asks
//     the model once through a Launch, with the skill from the checkout copy as
//     the appended system prompt, and wants a result file in the work area -
//     a regular file, no link, within MaxResult, one JSON value - that holds
//     neither the token nor the canary in its bytes or its decoded strings. It
//     returns the bytes it read and checked.
//
// Run takes the steps and prints one line: "finished", a constant class, or
// the preflight's Verdict, whose text is made of constants alone. No step's
// error text is printed: it can hold the CLI's output, the token or the canary.
// Once every step passed, Run hands the audit's bytes to its Result, the seam
// the publication works from.
// The CLI runs through the shared launcher ci/internal/claudecode with
// LaunchConfig, built from the run's Inputs alone, so nothing of the caller's
// environment reaches it. Every path is taken under a root that stands in for
// /, so a test lays the tree out under a temporary directory; SystemSteps are
// the real steps, which ci/auditrun runs with the root /.
//
// It is no part of the product: it sits under ci/internal, so Go's internal
// rule lets only packages under ci/ import it, and the depguard rule
// audit-library-importers names the ones that may. The strict depguard rule
// audit-library holds it to named standard packages and the launcher, and the
// forbidigo group audit-library-env keeps it from reading the environment or
// locating a home, config or cache directory (issue #277 round F); it names no
// process start, since the library starts bwrap and sudo.
package audit
