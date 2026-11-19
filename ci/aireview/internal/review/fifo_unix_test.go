//go:build unix

package review_test

import (
	"path/filepath"
	"syscall"
	"testing"
)

// newStartFIFO makes the FIFO the fake CLI signals its start through, and
// returns its path. A FIFO is what lets the start be observed without a sleep
// or a poll: the fake's write blocks until the test opens it, and the test's
// open blocks until the fake writes (issue #303). Its creation is Unix-only, so
// it lives here; fifo_other_test.go skips where there are no FIFOs.
func newStartFIFO(t *testing.T) string {
	t.Helper()

	fifo := filepath.Join(t.TempDir(), "started")

	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatalf("make the FIFO the fake CLI signals its start with: %v", err)
	}

	return fifo
}
