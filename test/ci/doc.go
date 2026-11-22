// Package ci holds behaviour tests of the scripts under .github/scripts, run
// against fixtures the test itself builds in a temporary directory: offline,
// with no credentials, unprivileged, and with the commands a script calls faked
// first on PATH.
//
// TestInstallClaudeCodeRefusesUnverifiedPackages runs
// .github/scripts/install-claude-code.sh, the installer of the AI reviewer's
// Claude Code CLI (PRD #161 milestone 4, ADR 0033), with a fake npm that serves
// a tarball built at run time and the npm sha512 integrity of its bytes. The
// cases pin what the installer refuses - a package that is not an exact version
// of a platform package, an integrity that is not an npm sha512-<base64> value,
// a tarball that does not match the pinned integrity, and a runner whose
// /etc/claude-code would apply a managed policy - each with the exit status, the
// output, that npm was not called when the installer refuses before packing, and
// that nothing was extracted. The one matching tarball installs package/claude
// alone, executable, and the installer prints the version line that binary prints.
// Whatever a case's exit, the installer has removed the mktemp -d directory it
// downloaded into, so the case's TMPDIR holds no entry once it returns: after
// the successful install and after every refusal, the integrity mismatch
// included. The installed package/claude is under the destination, not there.
// CLAUDE_CODE_INSTALL_ROOT, read by the script in place of /, points every case
// at a root in t.TempDir(), so no test touches /etc; the cases pin that
// GITHUB_ACTIONS cannot weaken that check.
// Each case's run gets an environment the harness builds for it - PATH with the
// fake npm first, HOME and TMPDIR in the case's temporary directory, LANG and
// LC_ALL as C - so no variable of the developing machine's own environment, a
// credential's included, reaches the script.
//
// TestAuditPathsScriptFencesAuditPullRequests runs
// .github/scripts/audit-paths.sh, the required path check of PRD #175
// milestone 2, against git repositories the test builds in t.TempDir(): a base
// commit and a head commit with the case's change. The cases pin that the fence
// applies to an audit pull request - one authored by shift2eu-audit[bot], exact
// match; one whose sender is that App, whose contents write lets it push a
// commit onto another author's pull request; and one whose head branch is under
// security-audit/, where the App pushes its reports - and that every other pull
// request passes whatever it changes. An audit pull request passes when its base
// branch is main and every changed path is under docs/security/, with no
// component starting with a dot, and every entry there a regular,
// non-executable file; and it fails, naming an offending path, for a change
// outside docs/security/ - an addition, a modification, a deletion, a rename
// (not detected, so the old path counts), a mode change, an executable file, a
// symlink or a gitlink under docs/security/ - for a path under docs/security/
// with a component starting with a dot, where a .gitattributes could set
// export-ignore on the reports a release archive ships, and for a docs-only pull
// request into any other base branch, which could be retargeted to main after
// the check passed. No line of a run's output starts with ::, a workflow
// command, whatever the case's file names hold - a newline in one, which a
// line-based diff would split, included. It fails closed, non-zero, when the
// author, the sender or the head branch is unset or empty, when the base branch
// is unset or empty for an audit pull request, and when BASE or HEAD is unset or
// empty, names no commit of the repository or is no full commit SHA - a branch
// name or a short SHA included. Each case's git commands and the check run in an
// environment the harness builds - HOME and TMPDIR in the case's temporary
// directory, the system and global git configuration off, LANG and LC_ALL as C
// - so no setting or credential of the developing machine's reaches a commit or
// a run.
//
// A script needs bash, tar, openssl, mktemp, cp, sh and gzip on PATH, and GNU
// coreutils' base64, the only one with -w0 for the unwrapped digest the
// installer compares; the test skips when one of them is missing. The path
// check needs bash and git, and skips when either is missing. The package
// has no production code, so it imports nothing, and its dependency rule holds
// it to that (test-docs-are-pure).
package ci
