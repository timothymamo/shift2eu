package lint_test

import "testing"

// TestCLITestImportersProbe runs depguard with the repository's .golangci.yml on
// a probe module laid out like the repository (testdata/clitestimporters/mod),
// for cli-only-in-named-tests (issue #345): every rule that holds production
// files leaves test files out (!$test), so before that rule a test of
// internal/web or internal/httpapi could import internal/cli and couple two
// driving adapters with lint green.
//
// The test files of the three named importers, internal/cli's own, the wire
// package's parity tests and test/apiparity's, import internal/cli with no want
// comment, as does cmd/shift2eu's main.go, a production file the rule does not
// judge. Each other test file that imports it ends in a want naming the rule:
// internal/web's, internal/httpapi's, cmd/shift2eu's, one in a package below
// internal/cli (the rule names directories, not trees), and the site's glue,
// which web-root-tests-never-run-the-cli refuses as well.
//
// make lint runs this probe with SHIFT2EU_LINT_PROBES=require on the host, and
// every entry of CI's lint-target matrix runs the step that sets it.
func TestCLITestImportersProbe(t *testing.T) {
	t.Parallel()

	runForbidigoProbe(t, forbidigoProbe{dir: "clitestimporters", depguard: true})
}
