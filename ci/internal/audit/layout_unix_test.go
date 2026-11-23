//go:build unix

// Pins the two entry types the layout's walk refuses that only a Unix host can
// lay down - a FIFO and a unix socket - inside the checkout copy and the checks'
// results (ADR 0040's milestone-2 amendment, issue #277, round D finding 3).
//
// They are here, behind the unix build tag, because a FIFO is made with
// syscall.Mkfifo, which windows and js/wasm do not have, and make lint
// type-checks every release target: the rest of the layout's cases stay in
// layout_test.go and run on every target.
package audit_test

import (
	"net"
	"os"
	"syscall"
	"testing"
)

// unixLayoutCases are the layout cases only a Unix host can lay down. The walk
// meets two entry types a checkout copy and the checks' results never hold, and
// refuses them: a FIFO and a socket are read or written by another process, so
// neither is a file the audit reads (issue #277, round D finding 3). Both are
// laid down read-only, so the type is the one defect the case names.
var unixLayoutCases = []layoutCase{
	{
		name:      "the checkout copy holds a FIFO",
		wantNamed: "srv/audit/checkout/pipe",
		mutate: mutateTree(checkout, func(t *testing.T, root string) {
			t.Helper()
			makeFIFO(t, pathOf(root, []string{"checkout", "pipe"}))
		}),
	},
	{
		// A short name: the socket's path is limited to 108 bytes. A socket
		// the checks' results hold in place of a report is the same defect.
		name:      "a socket",
		wantNamed: "srv/audit/checks/sock",
		mutate: mutateTree(checks, func(t *testing.T, root string) {
			t.Helper()
			makeSocket(t, pathOf(root, []string{"checks", "sock"}))
		}),
	},
}

// makeFIFO makes a FIFO at path, mode 0444: an entry of a type no checkout copy
// holds, laid down read-only so that the type is the one defect a case names.
func makeFIFO(t *testing.T, path string) {
	t.Helper()

	if err := syscall.Mkfifo(path, 0o444); err != nil {
		t.Fatalf("make a FIFO at %s: %v", path, err)
	}

	// Mkfifo's mode is filtered through the umask, so the owner's write bit a
	// shell with a 022 umask keeps would be the defect this case names.
	if err := os.Chmod(path, 0o444); err != nil { //nolint:gosec // G302: a read-only entry is the case's premise, the type being its one defect
		t.Fatalf("chmod the FIFO %s: %v", path, err)
	}
}

// makeSocket makes a unix socket at path, mode 0444, and closes the listener
// once the test is done: the socket's file is the entry a case names.
func makeSocket(t *testing.T, path string) {
	t.Helper()

	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("make a socket at %s: %v", path, err)
	}

	t.Cleanup(func() { _ = listener.Close() })

	// A socket is created writable, and a write bit would be the defect this
	// case names rather than the entry type it is about.
	if err := os.Chmod(path, 0o444); err != nil { //nolint:gosec // G302: a read-only entry is the case's premise, the type being its one defect
		t.Fatalf("chmod the socket %s: %v", path, err)
	}
}
