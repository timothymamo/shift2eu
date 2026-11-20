package lint_test

import "testing"

// TestCompositionRootTestsReachNoForeignCodeProbe runs depguard with the
// repository's .golangci.yml on a probe module laid out like the repository
// (testdata/compositionroottests/mod), whose composition root holds a
// _test.go file that imports the packages the composition-root-tests rule must
// refuse: go/build, net/http/cgi, os/exec, os/user, plugin, runtime/cgo and
// unsafe. Since issue #152 the driving adapter's _test.go file (internal/cli)
// holds the same imports, each with a want that names no rule, because a test
// of the CLI runs in the same process as the code the composition root hands
// the work to.
//
// The rule is lax, so its deny list alone decides, and it is the one depguard
// rule that holds the roots' _test.go files: composition-root and
// web-composition-root exclude them (!$test), and a test runs in the same
// process as the code under test. A _test.go file can start a program
// (os/exec), hand a path to a CGI handler (net/http/cgi), load a plugin (plugin), read a tree
// (go/build), call C (runtime/cgo), look a user up in the OS database (os/user,
// issue #152) or reach any package's symbol through unsafe and //go:linkname,
// which depguard sees as the import alone. Each line that ends in a want comment
// names the list, so the entry may pick its reason.
//
// syscall/js is in the rule's deny list and has no probe line: the package
// builds for js/wasm only, so a probe file that imports it would not type-check
// on the host or on any release target. The pin in
// test/architecture/depguardpin_test.go is what holds that entry; the probe
// covers the seven packages that build everywhere.
//
// make lint runs this probe with SHIFT2EU_LINT_PROBES=require on the host, and
// every entry of CI's lint-target matrix runs the step that sets it.
func TestCompositionRootTestsReachNoForeignCodeProbe(t *testing.T) {
	t.Parallel()

	runForbidigoProbe(t, forbidigoProbe{dir: "compositionroottests", depguard: true})
}
