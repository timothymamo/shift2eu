// Package docs checks the repository's documentation as plain tests, without
// the network (docs/README.md, "Writing a page"):
//
//   - every relative link and #anchor in README.md, docs/**/*.md and
//     .claude/skills/**/*.md resolves to a file and a heading that exist, and
//     no link names the repository's files by URL; external URLs are not
//     fetched;
//   - README.md and every page under docs/ start with the single # heading
//     that is its title;
//   - every page under docs/ is listed by an index: docs/README.md, or a
//     sub-index, a README.md in a directory directly below docs/ that
//     docs/README.md links (one level, no chains). A link to a directory
//     lists every page directly inside it, not those in its subdirectories,
//     so a dated audit report under docs/security/audits/ needs no index
//     edit (PRD #175, issue #280);
//   - README.md and SECURITY.md, the root pages every release archive ships,
//     link only to files those archives ship, so their links work in an
//     unpacked archive;
//   - no shell command block (tagged bash, sh, shell, console, or untagged) in
//     CLAUDE.md or in a skill writes or removes a file through the shell
//     (heredoc, redirection, tee, sed -i, perl -i) or removes a path held in a
//     variable (CLAUDE.md, rule 9; issue #251). The list covers the forms seen
//     in #251 and is not complete: `python3 -c`, `dd`, `cp`/`mv` and
//     `find -delete` write or remove a file and are not caught. The prompts
//     in .dot-agent-deck.toml are not scanned: they quote the forbidden forms
//     as examples in prose;
//   - CLAUDE.md's `## Rules` section holds a numbered rule that opens with
//     "Signal only what you started." and names pgrep, pidof and pkill, and
//     every skill that tells an agent to run the checks (`make check`,
//     `make test`, `go test`, `make lint`, `govulncheck`, or the dispatcher's
//     `Checks on a merged scratch tree`) says
//     "Signal only what you started (CLAUDE.md rule N)", with the number N the
//     rules list gives that rule, so a renumbered rule cannot leave a pointer
//     behind (issue #465: a unit's coder SIGTERMed the process group of a
//     `make` it found by name, possibly another unit's run);
//   - docs/site-runbook.md's R5 stops on both forms of the bucket's
//     AccessDenied, the S3 API's XML body and the website endpoint's HTML page,
//     and its outcome items name which read rule a 200 means (issue #535).
//
// make test, and so make check, run it. The link checker is
// test/internal/mdlink.
package docs
