// Pins the audit directory's layout, the third step of an audit run (ADR
// 0040's milestone-2 amendment, issue #277, "The audit directory"): the
// directory exists where the model-free step made it, with the checkout copy
// and the checks' results holding no write bit, an empty work area with its
// tmp/, an empty state home/ and tmp/, the state area's bin/ and out/, and the
// wrapper at bin/audit-exec with mode 0555.
//
// Every path the layout puts something of its own at is checked as an entry: a
// symbolic link there - the state area's home/ in particular, which holds the
// CLI's configuration and transcripts - is a layout the model could read from
// elsewhere, so it is refused rather than followed. The library takes a root
// standing in for /, so a test lays the tree out under a temporary root.
package audit_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/timothymamo/shift2eu/ci/internal/audit"
)

// auditPath is the audit directory's path under a root that stands in for /.
const auditPath = "srv/audit"

// The audit directory's paths under a root, each a part of the layout.
var (
	checkout  = []string{"checkout"}
	checks    = []string{"checks"}
	work      = []string{"work"}
	workTmp   = []string{"work", "tmp"}
	state     = []string{"state"}
	home      = []string{"state", "home"}
	stateTmp  = []string{"state", "tmp"}
	stateBin  = []string{"state", "bin"}
	stateOut  = []string{"state", "out"}
	wrapper   = []string{"bin", "audit-exec"}
	claudeBin = []string{"state", "bin", "claude"}
	binDir    = []string{"bin"}
)

// TestCheckLayoutTakesTheADRsDirectory pins the layout that passes. Nothing is
// created by the check itself: the model-free step lays the directory out, so a
// check that made a missing directory would hide the step that did not run.
func TestCheckLayoutTakesTheADRsDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeAuditTree(t, root)

	if err := audit.CheckLayout(root); err != nil {
		t.Fatalf("CheckLayout(%q) error = %v, want none: the tree is the one ADR 0040 lays out", root, err)
	}
}

// TestCheckLayoutRefusesAnythingElse pins every way the tree can differ from
// the ADR's: a directory or the wrapper missing, a write bit on the checkout
// copy or the checks' results, an empty area holding something, a wrapper of
// another mode, and a symbolic link wherever the layout puts something of its
// own.
func TestCheckLayoutRefusesAnythingElse(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// mutate breaks the tree the case is about.
		mutate func(t *testing.T, root string)
	}{
		{name: "the checkout copy is missing", mutate: gone(checkout)},
		{name: "the checks' results are missing", mutate: gone(checks)},
		{name: "the work area is missing", mutate: gone(work)},
		{name: "the work area's tmp is missing", mutate: gone(workTmp)},
		{name: "the state area is missing", mutate: gone(state)},
		{name: "the state area's home is missing", mutate: gone(home)},
		{name: "the state area's tmp is missing", mutate: gone(stateTmp)},
		{name: "the state area's bin is missing", mutate: gone(stateBin)},
		{name: "the state area's out is missing", mutate: gone(stateOut)},
		{name: "the wrapper is missing", mutate: gone(wrapper)},

		// 0700 keeps the write bit the ADR's layout must not have, without
		// handing it to group or other.
		{name: "the checkout copy keeps a write bit", mutate: chmodPath(0o700, checkout)},
		{name: "the checks' results keep a write bit", mutate: chmodPath(0o700, checks)},
		{name: "the wrapper is writable", mutate: chmodPath(0o700, wrapper)},
		{name: "the wrapper is read-only", mutate: chmodPath(0o500, wrapper)},

		{
			name: "the work area holds an entry of its own",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				writeFile(t, filepath.Join(pathOf(root, work), "result.json"), "{}", 0o600)
			},
		},
		{
			name: "the state area's home is not empty",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				writeFile(t, filepath.Join(pathOf(root, home), ".claude.json"), "{}", 0o600)
			},
		},
		{
			name: "the state area's tmp is not empty",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				writeFile(t, filepath.Join(pathOf(root, stateTmp), "leftover"), "x", 0o600)
			},
		},

		{name: "the wrapper is a symbolic link", mutate: linkElsewhere(wrapper)},
		{name: "the checkout copy is a symbolic link", mutate: linkElsewhere(checkout)},
		{name: "the checks' results are a symbolic link", mutate: linkElsewhere(checks)},
		{name: "the work area is a symbolic link", mutate: linkElsewhere(work)},
		{name: "the work area's tmp is a symbolic link", mutate: linkElsewhere(workTmp)},
		{name: "the state area is a symbolic link", mutate: linkElsewhere(state)},
		{name: "the state area's home is a symbolic link", mutate: linkElsewhere(home)},
		{name: "the state area's tmp is a symbolic link", mutate: linkElsewhere(stateTmp)},
		{name: "the state area's out is a symbolic link", mutate: linkElsewhere(stateOut)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			writeAuditTree(t, root)
			tc.mutate(t, root)

			if err := audit.CheckLayout(root); err == nil {
				t.Errorf("CheckLayout(%q) = nil, want a refusal: %s is no layout ADR 0040 gives", root, tc.name)
			}
		})
	}
}

// pathOf returns the path of parts under root's audit directory.
func pathOf(root string, parts []string) string {
	return filepath.Join(append([]string{root, auditPath}, parts...)...)
}

// gone returns a mutation that deletes the entry parts names.
func gone(parts []string) func(t *testing.T, root string) {
	return func(t *testing.T, root string) {
		t.Helper()
		removeAll(t, pathOf(root, parts))
	}
}

// chmodPath returns a mutation that gives the entry parts names mode.
func chmodPath(mode os.FileMode, parts []string) func(t *testing.T, root string) {
	return func(t *testing.T, root string) {
		t.Helper()

		if err := os.Chmod(pathOf(root, parts), mode); err != nil {
			t.Fatalf("chmod %v to %#o: %v", parts, mode, err)
		}
	}
}

// linkElsewhere returns a mutation that replaces the entry parts names with a
// symbolic link to a directory of its own, so a check that followed it would
// read a tree that is not the audit directory's.
func linkElsewhere(parts []string) func(t *testing.T, root string) {
	return func(t *testing.T, root string) {
		t.Helper()

		removeAll(t, pathOf(root, parts))

		target := filepath.Join(t.TempDir(), "elsewhere")
		makeDir(t, target)
		makeSymlink(t, target, pathOf(root, parts))
	}
}

// writeAuditTree lays the audit directory out under root as the model-free step
// leaves it before the launcher runs: every path the ADR's table names, the
// binary the checksum step extracted into the state area's bin/, and the
// wrapper the model job built there.
func writeAuditTree(t *testing.T, root string) {
	t.Helper()

	for _, parts := range [][]string{
		checkout,
		checks,
		work,
		workTmp,
		state,
		home,
		stateTmp,
		stateBin,
		stateOut,
		binDir,
	} {
		makeDir(t, pathOf(root, parts))
	}

	writeFile(t, pathOf(root, wrapper), "#!/bin/sh\nexit 126\n", 0o555)
	writeFile(t, pathOf(root, claudeBin), claudeScript, 0o500)

	// The checkout copy and the checks' results are read-only to the model, so
	// the directories themselves lose their write bit after they are filled.
	for _, parts := range [][]string{checkout, checks} {
		if err := os.Chmod(pathOf(root, parts), 0o555); err != nil { //nolint:gosec // G302: 0555 is the ADR's mode for the checkout copy and the checks' results, writable by nobody
			t.Fatalf("chmod %v: %v", parts, err)
		}
	}
}

// removeAll deletes path and everything under it.
func removeAll(t *testing.T, path string) {
	t.Helper()

	if err := os.RemoveAll(path); err != nil {
		t.Fatalf("remove %s: %v", path, err)
	}
}
