//go:build unix

package claudecode_test

import (
	"syscall"
	"testing"
)

// makeGateFIFO makes path the FIFO the fake CLI waits at. A FIFO is what lets
// "the launcher stopped this run" be observed without a sleep and without a
// poll: the fake's open of the gate blocks until the test holds the same FIFO
// open for writing, and the test's release is already in it when the fake
// arrives (issue #592). Making one is Unix-only, so it lives here;
// gate_other_test.go skips the rows that need one.
func makeGateFIFO(t *testing.T, path string) {
	t.Helper()

	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("make the fake CLI's gate a FIFO: %v", err)
	}
}
