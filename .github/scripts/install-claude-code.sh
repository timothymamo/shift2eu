#!/usr/bin/env bash
# Installs the Claude Code CLI the AI reviewer asks its model through (PRD #161 milestone 4, ADR 0033),
# run by .github/workflows/ai-review.yml before any secret is in a step's environment:
#
#   install-claude-code.sh <directory> <platform package>@<X.Y.Z> <sha512-... npm integrity>
#
# It downloads the platform package's tarball with npm pack, which runs no install script, compares it
# with the pinned npm integrity, and only then extracts its one binary, package/claude, into
# <directory>, where the reviewer runs it as <directory>/package/claude. A tarball that does not match
# fails the run and nothing of it is extracted. The tarball is downloaded into a directory of its own
# from mktemp -d, which an EXIT trap removes on every exit, a failed one included (issue #451);
# <directory> stays. The meta package @anthropic-ai/claude-code only picks a platform package in a
# postinstall script, so it is never installed. The workflow pins the version (CLAUDE_CODE_VERSION,
# under a # renovate: comment) and the integrity (CLAUDE_CODE_INTEGRITY) in the install step's env: and
# passes both here: a Renovate bump of the version must replace the integrity too (npm view
# <package>@<version> dist.integrity).
#
# It fails when /etc/claude-code exists: the CLI applies an admin-managed policy from there
# (managed-settings.json, managed plugins' hooks) under every flag, --safe-mode and --setting-sources ""
# included. The GitHub-hosted runner image ships none; one that does is noticed rather than applied.
#
# CLAUDE_CODE_INSTALL_ROOT, a test hook only test/ci sets (issue #275), stands in for / in that check, so
# a test can put a policy at <root>/etc/claude-code unprivileged; unset, the check reads /etc/claude-code.
# It can only move the check, never drop it, and the script refuses it whenever GITHUB_ACTIONS is true,
# even when it is empty, so no workflow step can take effect through it.
set -euo pipefail

if [ -n "${CLAUDE_CODE_INSTALL_ROOT+x}" ] && [ "${GITHUB_ACTIONS:-}" = "true" ]; then
	echo "::error title=Claude Code CLI::CLAUDE_CODE_INSTALL_ROOT is a test hook and is refused in a GitHub Actions run; unset it so the managed-policy check reads /etc/claude-code (ADR 0033)"
	exit 1
fi

policy="${CLAUDE_CODE_INSTALL_ROOT:-/}"
policy="${policy%/}/etc/claude-code"

if [ -e "${policy}" ]; then
	echo "::error title=Claude Code CLI::${policy} exists on this runner; its managed policy would apply to the reviewer's CLI under every flag (ADR 0033)"
	exit 1
fi

if [ "$#" -ne 3 ]; then
	echo "usage: $0 <directory> <platform package>@<X.Y.Z> <sha512 integrity>" >&2
	exit 2
fi

dest=$1
package=$2
integrity=$3

if [[ ! "${package}" =~ ^@anthropic-ai/claude-code-[a-z0-9-]+@[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
	echo "install-claude-code: ${package} is not an exact version of a Claude Code platform package" >&2
	exit 2
fi

if [[ ! "${integrity}" =~ ^sha512-[A-Za-z0-9+/]+=*$ ]]; then
	echo "install-claude-code: the integrity is not an npm sha512-<base64> integrity" >&2
	exit 2
fi

# The trap is set before mktemp -d, so a signal right after it returns still removes the directory. The
# cleanup never decides the verdict: a failed rm leaves the script's own exit status as it was.
download=""
remove_download() {
	if [ -n "${download:-}" ] && [ -d "${download}" ]; then
		rm -rf -- "${download}" || true
	fi
}
trap remove_download EXIT
download=$(mktemp -d)
mkdir -p "${dest}"

tgz=$(npm pack --ignore-scripts --silent --pack-destination "${download}" "${package}")
actual="sha512-$(openssl dgst -sha512 -binary "${download}/${tgz}" | base64 -w0)"

if [ "${actual}" != "${integrity}" ]; then
	echo "::error title=Claude Code CLI::${package}: the downloaded tarball does not match the pinned sha512 integrity"
	exit 1
fi

tar -xzf "${download}/${tgz}" -C "${dest}" package/claude
DISABLE_AUTOUPDATER=1 "${dest}/package/claude" --version
