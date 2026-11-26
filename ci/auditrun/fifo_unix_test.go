//go:build unix

// Pins the FIFO half of the auditor's round-4 L1 in ci/auditrun (issues #615,
// #614): a file command whose path exists before the run but is no regular file
// the run can open - here a named pipe, which a real runner never lays down - is
// refused with the usage exit before the first step. Making a FIFO is
// syscall.Mkfifo's work, which windows and js/wasm do not have, so it lives here;
// fifo_other_test.go skips the row that needs one, and make lint type-checks this
// package for the other targets too.
package main

import (
	"syscall"
	"testing"
)

// makeFIFO makes path a named pipe: an entry that exists and is no regular file
// the run can open. Nothing ever opens it, so no test waits for a writer.
func makeFIFO(t *testing.T, path string) {
	t.Helper()

	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("make the FIFO at %s: %v", path, err)
	}
}
