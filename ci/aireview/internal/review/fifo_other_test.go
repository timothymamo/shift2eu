//go:build !unix

package review_test

import "testing"

// newStartFIFO skips: the fake CLI is a /bin/sh script and its start is
// signalled through a FIFO, so the test that needs one runs on the Unix release
// targets only. make lint still compiles this package for the others (issue
// #303), which is why the Unix body, syscall.Mkfifo and all, sits behind a build
// constraint in fifo_unix_test.go.
func newStartFIFO(t *testing.T) string {
	t.Helper()

	t.Skip("the fake CLI is a /bin/sh script whose start signal needs a FIFO: this test runs on Unix only")

	return ""
}
