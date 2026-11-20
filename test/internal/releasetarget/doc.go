// Package releasetarget reads the platforms the release builds from
// .goreleaser.yaml (issue #40, ADR 0012), so that the test packages that hold
// something to every release target read one list, not copies of it:
// test/architecture checks the dependency rule package by package and pins make
// lint and CI's lint matrix to the list, and test/lint's
// TestLintLoadsEveryGoFile fails on a Go file that no release target loads
// (issue #82).
//
// Read returns the goos x goarch product of the document's builds, sorted: the
// CLI's alone, since the HTTP API is no longer a release binary (issue #393).
// It fails closed: it refuses every key, hook, env entry, flag and YAML
// feature that could change what the release compiles and that it does not
// read, so a change to how the release is built fails the tests until the
// reader is taught it. Main is the only main package the release may build,
// ServerMain the one it may not, and Env
// the environment every build must set, exactly: CGO_ENABLED=0, GOWORK=off
// and GOFLAGS=-mod=readonly (issue #102). The
// document is read with test/internal/yamlread.
//
// The package is internal to test: it is not an API of the module.
package releasetarget
