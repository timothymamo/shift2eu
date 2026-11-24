//go:build unix

package github_test

// Group E of PR 4b on a Unix host: a FIFO where the result file should be. A
// FIFO is no regular file, and opening one blocks until a writer appears, so
// LoadResult refuses it before it opens anything. The file is Unix-only, as
// internal/cli/output_unix_test.go is, because syscall.Mkfifo is: the case
// joins the table of load_test.go on the hosts that have FIFOs.

import (
	"errors"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/github"
	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// TestLoadResultRefusesAFIFO pins the non-regular file: a FIFO is no result,
// and the check refuses it rather than wait on a writer.
func TestLoadResultRefusesAFIFO(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "result.json")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("make the FIFO %s: %v", path, err)
	}

	if _, err := github.LoadResult(path, fixtureCommit); !errors.Is(err, publish.ErrResultInvalid) {
		t.Fatalf("LoadResult of a FIFO = %v, want an error wrapping publish.ErrResultInvalid", err)
	}
}
