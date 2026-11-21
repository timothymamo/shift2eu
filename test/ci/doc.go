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
// output, that npm was not called and that nothing was extracted. The one
// matching tarball installs package/claude alone, executable, and the installer
// prints the version line that binary prints. CLAUDE_CODE_INSTALL_ROOT, read by
// the script in place of /, points every case at a root in t.TempDir(), so no
// test touches /etc; the cases pin that GITHUB_ACTIONS cannot weaken that check.
//
// A script needs bash, tar, openssl and base64 on PATH, and the test skips when
// one of them is missing. The package has no production code, so it imports
// nothing, and its dependency rule holds it to that (test-docs-are-pure).
package ci
