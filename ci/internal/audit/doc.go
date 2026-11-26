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
//     parser cannot place, and wanting the sessions' working directories
//     cwd-preflight/ and cwd-audit/, siblings of the work area, empty real
//     directories: under the subprocess environment scrub, Claude Code
//     2.1.332 leaves empty placeholder files and directories in the directory
//     it starts in, so it never starts in the work area, and it reads that
//     directory at session start (a CLAUDE.md there is loaded), so each
//     session has its own (issues #615, #614).
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
//     must refuse them on every run. Probe 5's token-absence call tests the
//     variable positively in each of its two branches, `[[ -v
//     CLAUDE_CODE_OAUTH_TOKEN ]] && ... tokenset; [[ ! -v
//     CLAUDE_CODE_OAUTH_TOKEN ]] && ... nonexistent/`, neither a fallback,
//     and holds no $, which Claude Code 2.1.332 refuses before it runs (issue
//     #613); Judge knows it by the mark PreflightPlan sets, and it passes only
//     as an error holding cat's own line for /srv/audit/work/nonexistent/ and
//     no tokenset, of a call the CLI ran.
//     Judge decodes the message of assistant and user elements, a user
//     element's tool_result_meta and a system/permission_denied element's
//     tool_use_id, and skips every other element, whatever it holds; a call
//     that a permission_denied element or a non_execution_kind records as
//     never run fails probe 5's token-absence call. A line it cannot read is
//     refused with its 1-based number and a class from a fixed list, matched
//     on the line's first 4 KiB, never with any of its bytes (issue #590). It
//     walks each element's top-level object, and each tool_result_meta entry,
//     once, token by token, and refuses one that names a key twice or names a
//     key it reads in another case, which encoding/json would read
//     case-insensitively, keeping the last. Told the launch's permission mode
//     (default, which Claude Code 2.1.332 runs under the subprocess
//     environment scrub, issue #592), it first reads the transcript's
//     system/init element through the launcher's ModeCheck and refuses one
//     that reports another mode, or none.
//  6. Audit checks that the work area again holds only its empty tmp/ and
//     that cwd-audit/ is still an empty real directory, asks the model once
//     through a Launch, with the skill from the checkout copy as the appended
//     system prompt, fails on any change of the digest of the runner's
//     file-command directory - the $GITHUB_ENV file's, beside $GITHUB_PATH,
//     $GITHUB_OUTPUT, $GITHUB_STATE and the step summary: every entry's
//     name, type (Lstat, no link followed) and bytes, every entry digested,
//     the step summary included, at most 64 entries and 4 MiB, a directory
//     over either refused before the run - across that run (issues #615,
//     #614), and wants a result file in the work area - a regular file, no
//     link, within MaxResult, one JSON value - that holds neither the token
//     nor the canary in its bytes or its decoded strings. It returns the
//     bytes it read and checked. ci/auditrun holds the directory's paths
//     before the steps: it refuses a file command that is unset, outside the
//     $GITHUB_ENV file's directory (FileCommandDir), relative or not already
//     clean (IsCleanAbsolute), and one that exists but is no regular file - a
//     directory, a link, a FIFO - (CheckFileCommands, Lstat alone, before it
//     opens the summary with OpenAppend, which follows no link). It also
//     records the five file commands before the steps (RecordFileCommands:
//     each one's identity, Lstat and an os.SameFile-checked descriptor, and
//     the sha256 of its content, capped as the digest is; an existing entry
//     it cannot open and read fails the record), and after them
//     FileCommands.Repair leaves every one the steps changed - another entry
//     at its path, or content other than the recorded one followed by what
//     the run appended (Appended) - as an empty regular file: truncated
//     through its descriptor, or replaced by an empty file renamed over the
//     path, an empty directory there removed first, never following a link,
//     each repair verified with an Lstat. It tries every changed file
//     command, whatever failed before it, and joins one RepairError per
//     failure (issues #615, #614).
//
// Run takes the steps and prints one line: "finished", a constant class, the
// preflight's Verdict, whose text is made of constants alone, or, when the
// preflight's or the audit's CLI run was stopped by the launcher's
// permission-mode check, "refused: " and the *claudecode.PermissionModeError's
// fixed sentence (issue #592), which the steps keep through their wrapping. No
// step's error text is printed: it can hold the CLI's output, the token or the
// canary. Once every step passed, Run hands the audit's bytes to its Result,
// the seam the publication works from.
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
