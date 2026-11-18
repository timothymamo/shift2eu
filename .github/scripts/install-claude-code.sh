#!/usr/bin/env bash
# Installs the Claude Code CLI the AI reviewer asks its model through (PRD #161 milestone 4, ADR 0033),
# run by .github/workflows/ai-review.yml before any secret is in a step's environment:
#
#   install-claude-code.sh <directory> <platform package>@<X.Y.Z> <sha512-... npm integrity>
#
# It downloads the platform package's tarball with npm pack, which runs no install script, compares it
# with the pinned npm integrity, and only then extracts its one binary, package/claude, into
# <directory>, where the reviewer runs it as <directory>/package/claude. A tarball that does not match
# fails the run and nothing of it is extracted. The meta package @anthropic-ai/claude-code only picks a
# platform package in a postinstall script, so it is never installed. The workflow pins the version
# (CLAUDE_CODE_VERSION, under a # renovate: comment) and the integrity (CLAUDE_CODE_INTEGRITY) in the
# install step's env: and passes both here: a Renovate bump of the version must replace the integrity
# too (npm view <package>@<version> dist.integrity).
#
# It fails when /etc/claude-code exists: the CLI applies an admin-managed policy from there
# (managed-settings.json, managed plugins' hooks) under every flag, --safe-mode and --setting-sources ""
# included. The GitHub-hosted runner image ships none; one that does is noticed rather than applied.
set -euo pipefail

if [ -e /etc/claude-code ]; then
	echo "::error title=Claude Code CLI::/etc/claude-code exists on this runner; its managed policy would apply to the reviewer's CLI under every flag (ADR 0033)"
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
