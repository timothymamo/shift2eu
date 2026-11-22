// Package audit is the logic of the scheduled security audit's model step
// (ADR 0040's milestone-2 amendment, issue #277, PRD #175 milestone 2): the six
// steps ci/auditrun takes, in order, each failing the run before the next.
//
//  1. CheckManagedSettings refuses a root holding a Claude Code managed-settings
//     path, checked with Lstat.
//  2. InstallClaude checks the kept tarball's sha512 against the pin's npm
//     integrity and only then extracts its one entry, package/claude, mode 0500.
//  3. CheckLayout checks the audit directory under /srv/audit as the ADR lays
//     it out, refusing a symbolic link anywhere in it.
//  4. CheckBubblewrap starts `bwrap --unshare-all --die-with-parent --ro-bind /
//     / /bin/true` once.
//  5. Preflight has the model make PreflightPlan's calls - a control that reads
//     a nonce back, then the six probes - and Judge, a pure function of the
//     plan, the nonce, the transcript and the effects the launcher observes
//     itself, passes only when the control passed and every probe was refused.
//  6. Audit asks the model once, with the skill from the checkout copy as the
//     appended system prompt, and wants the result file in the work area.
//
// Run takes the steps and prints one line: "finished", a constant class, or
// the preflight's Verdict, whose text is made of constants alone. No step's
// error text is printed: it can hold the CLI's output, the token or the canary.
// The CLI runs through the shared launcher ci/internal/claudecode with
// LaunchConfig, built from the run's Inputs alone, so nothing of the caller's
// environment reaches it. Every path is taken under a root that stands in for
// /, so a test lays the tree out under a temporary directory; SystemSteps are
// the real steps, which ci/auditrun runs with the root /.
//
// It is no part of the product: it sits under ci/internal, so Go's internal
// rule lets only packages under ci/ import it, and the depguard rule
// audit-library-importers names the ones that may. The strict depguard rule
// audit-library holds it to named standard packages and the launcher.
package audit
