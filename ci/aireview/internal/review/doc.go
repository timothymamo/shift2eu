// Package review is the AI reviewer of PRD #161 milestone 4 (ADR 0033): it
// reads a pull request through the GitHub API as data, asks Claude Opus 5.5
// for findings with main's CLAUDE.md and review skills and no tools, decides
// the review's event itself from those findings, posts one review and
// dismisses its own earlier change requests once nothing blocking remains.
//
// It is no part of the product: only ci/aireview imports it (Go's internal
// rule), and neither release binary can reach it. It reads no file and no
// environment variable; ci/aireview hands it the settings, main's
// instructions and the HTTP clients, and the tests hand it httptest servers
// as base URLs, so no test reaches the network.
//
// The poster supports exactly two events, COMMENT and REQUEST_CHANGES, and
// refuses any other before it makes an HTTP call: the reviewer never
// approves. Everything taken from the pull request is untrusted data, kept
// out of the system prompt and delimited in the user message.
package review
