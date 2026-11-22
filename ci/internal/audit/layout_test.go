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
// copy or the checks' results, an empty layout holding something, a wrapper of
// another mode, and a symbolic link wherever the layout puts something of its
// own.
func TestCheckLayoutRefusesAnythingElse(t *testing.T) {
	t.Parallel()

	at := func(parts ...string) func(root string) string {
		return func(root string) string {
			return filepath.Join(append([]string{root, auditPath}, parts...)...)
		}
	}

	checkout := at("checkout")
	checks := at("checks")
	work := at("work")
	workTmp := at("work", "tmp")
	state := at("state")
	stateHome := at("state", "home")
	stateTmp := at("state", "tmp")
	stateBin := at("state", "bin")
	stateOut := at("state", "out")
	wrapper := at("bin", "audit-exec")

	cases := []struct {
		name string
		// mutate breaks the tree the case is about.
		mutate func(t *testing.T, root string)
	}{
		{
			name:   "the checkout copy is missing",
			mutate: func(t *testing.T, root string) { removeAll(t, checkout(root)) },
		},
		{
			name:   "the checks' results are missing",
			mutate: func(t *testing.T, root string) { removeAll(t, checks(root)) },
		},
		{
			name:   "the work area is missing",
			mutate: func(t *testing.T, root string) { removeAll(t, work(root)) },
		},
		{
			name:   "the work area's tmp is missing",
			mutate: func(t *testing.T, root string) { removeAll(t, workTmp(root)) },
		},
		{
			name:   "the state area is missing",
			mutate: func(t *testing.T, root string) { removeAll(t, state(root)) },
		},
		{
			name:   "the state area's home is missing",
			mutate: func(t *testing.T, root string) { removeAll(t, stateHome(root)) },
		},
		{
			name:   "the state area's tmp is missing",
			mutate: func(t *testing.T, root string) { removeAll(t, stateTmp(root)) },
		},
		{
			name:   "the state area's bin is missing",
			mutate: func(t *testing.T, root string) { removeAll(t, stateBin(root)) },
		},
		{
			name:   "the state area's out is missing",
			mutate: func(t *testing.T, root string) { removeAll(t, stateOut(root)) },
		},
		{
			name:   "the wrapper is missing",
			mutate: func(t *testing.T, root string) { removeAll(t, wrapper(root)) },
		},
		{
			name: "the checkout copy keeps a write bit",
			mutate: func(t *testing.T, root string) {
				if err := os.Chmod(checkout(root), 0o755); err != nil {
					t.Fatalf("chmod the checkout copy: %v", err)
				}
			},
		},
		{
			name: "the checks' results keep a write bit",
			mutate: func(t *testing.T, root string) {
				if err := os.Chmod(checks(root), 0o755); err != nil {
					t.Fatalf("chmod the checks' results: %v", err)
				}
			},
		},
		{
			name: "the work area holds an entry of its own",
			mutate: func(t *testing.T, root string) {
				writeFile(t, filepath.Join(work(root), "result.json"), "{}", 0o600)
			},
		},
		{
			name: "the state area's home is not empty",
			mutate: func(t *testing.T, root string) {
				writeFile(t, filepath.Join(stateHome(root), ".claude.json"), "{}", 0o600)
			},
		},
		{
			name: "the state area's tmp is not empty",
			mutate: func(t *testing.T, root string) {
				writeFile(t, filepath.Join(stateTmp(root), "leftover"), "x", 0o600)
			},
		},
		{
			name: "the wrapper is writable",
			mutate: func(t *testing.T, root string) {
				if err := os.Chmod(wrapper(root), 0o755); err != nil {
					t.Fatalf("chmod the wrapper: %v", err)
				}
			},
		},
		{
			name: "the wrapper is read-only",
			mutate: func(t *testing.T, root string) {
				if err := os.Chmod(wrapper(root), 0o500); err != nil {
					t.Fatalf("chmod the wrapper: %v", err)
				}
			},
		},
		{
			name: "the wrapper is a symbolic link",
			mutate: func(t *testing.T, root string) {
				linkElsewhere(t, wrapper(root))
			},
		},
		{
			name: "the checkout copy is a symbolic link",
			mutate: func(t *testing.T, root string) {
				linkElsewhere(t, checkout(root))
			},
		},
		{
			name: "the checks' results are a symbolic link",
			mutate: func(t *testing.T, root string) {
				linkElsewhere(t, checks(root))
			},
		},
		{
			name: "the work area is a symbolic link",
			mutate: func(t *testing.T, root string) {
				linkElsewhere(t, work(root))
			},
		},
		{
			name: "the work area's tmp is a symbolic link",
			mutate: func(t *testing.T, root string) {
				linkElsewhere(t, workTmp(root))
			},
		},
		{
			name: "the state area is a symbolic link",
			mutate: func(t *testing.T, root string) {
				linkElsewhere(t, state(root))
			},
		},
		{
			name: "the state area's home is a symbolic link",
			mutate: func(t *testing.T, root string) {
				linkElsewhere(t, stateHome(root))
			},
		},
		{
			name: "the state area's tmp is a symbolic link",
			mutate: func(t *testing.T, root string) {
				linkElsewhere(t, stateTmp(root))
			},
		},
		{
			name: "the state area's out is a symbolic link",
			mutate: func(t *testing.T, root string) {
				linkElsewhere(t, stateOut(root))
			},
		},
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

// writeAuditTree lays the audit directory out under root as the model-free step
// leaves it before the launcher runs: every path the ADR's table names, the
// binary the checksum step extracted into the state area's bin/, and the
// wrapper the model job built there.
func writeAuditTree(t *testing.T, root string) {
	t.Helper()

	dir := filepath.Join(root, auditPath)

	for _, sub := range []string{
		"checkout",
		"checks",
		"work",
		"work/tmp",
		"state",
		"state/home",
		"state/tmp",
		"state/bin",
		"state/out",
		"bin",
	} {
		makeDir(t, filepath.Join(dir, sub))
	}

	writeFile(t, filepath.Join(dir, "bin/audit-exec"), "#!/bin/sh\nexit 126\n", 0o555)
	writeFile(t, filepath.Join(dir, "state/bin/claude"), claudeScript, 0o500)

	// The checkout copy and the checks' results are read-only to the model, so
	// the directories themselves lose their write bit after they are filled.
	for _, sub := range []string{"checkout", "checks"} {
		if err := os.Chmod(filepath.Join(dir, sub), 0o555); err != nil {
			t.Fatalf("chmod %s: %v", sub, err)
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

// linkElsewhere replaces path with a symbolic link to a directory of its own, so
// a check that followed it would read a tree that is not the audit directory's.
func linkElsewhere(t *testing.T, path string) {
	t.Helper()

	removeAll(t, path)

	target := filepath.Join(t.TempDir(), "elsewhere")
	makeDir(t, target)
	makeSymlink(t, target, path)
}
