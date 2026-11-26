//go:build !unix

package main

import "testing"

// makeFIFO skips: it makes a named pipe, which a Windows or js/wasm host lays
// down no such way, so the row of TestRunRefusesEveryUsageAndInputError that
// needs one runs on the Unix release targets only. make lint still compiles this
// package for the others, which is why the Unix body, syscall.Mkfifo and all,
// sits behind a build constraint in fifo_unix_test.go. The path is not used here:
// the two bodies have one signature.
func makeFIFO(t *testing.T, _ string) {
	t.Helper()

	t.Skip("a named pipe is made with syscall.Mkfifo: this row runs on Unix only")
}
