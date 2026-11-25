//go:build !unix

package claudecode_test

import "testing"

// makeGateFIFO skips: the fake CLI is a /bin/sh script, and the run is stopped
// through the FIFO that script waits at, so the rows needing a gate run on the
// Unix release targets only. make lint still compiles this package for the
// others, which is why the Unix body, syscall.Mkfifo and all, sits behind a
// build constraint in gate_unix_test.go (issue #592, as issue #303 did for the
// reviewer's FIFO). The gate's path is not used here: the two bodies have one
// signature.
func makeGateFIFO(t *testing.T, _ string) {
	t.Helper()

	t.Skip("the fake CLI is a /bin/sh script that waits at a FIFO: this test runs on Unix only")
}
