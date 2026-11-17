// Package docs checks the repository's documentation as plain tests, without
// the network (docs/README.md, "Writing a page"):
//
//   - every relative link and #anchor in README.md, docs/**/*.md and
//     .claude/skills/**/*.md resolves to a file and a heading that exist, and
//     no link names the repository's files by URL; external URLs are not
//     fetched;
//   - README.md and every page under docs/ start with the single # heading
//     that is its title;
//   - docs/README.md, the index, links to every page under docs/;
//   - README.md links only to files the release archives in .goreleaser.yaml
//     ship, so its links work in an unpacked archive;
//   - no command block in a skill writes a file through the shell (heredoc,
//     redirection, tee, sed -i) or removes a path held in a variable
//     (CLAUDE.md, rule 9; issue #251).
//
// make test, and so make check, run it. The link checker is
// test/internal/mdlink.
package docs
