// Pins the descriptor audit.OpenAppend returns for an existing regular file
// command (issues #615, #614): it is opened O_RDWR, so the run can read the file
// it appends its summary line to, and so a named pipe a concurrent process
// swapped in between the Lstat and the open is opened without blocking - a
// read-only open of a pipe with no writer waits for one - and then refused by the
// os.SameFile check. A descriptor opened O_WRONLY, the shape this file command
// had before, answers a read with EBADF, which is what the test below tells
// apart.
//
// The file command's other ends - a path that exists but is no regular file, and
// a link's target - are pinned through ci/auditrun's run.
package audit_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/timothymamo/shift2eu/ci/internal/audit"
)

// TestOpenAppendReadsTheFileItAppendsTo pins the open audit.OpenAppend makes of an
// existing regular file (issues #615, #614): the descriptor it returns reads the
// file's content from offset 0 as well as appends to its end, both through the one
// descriptor, and the file it names is the step's own.
func TestOpenAppendReadsTheFileItAppendsTo(t *testing.T) {
	t.Parallel()

	const (
		held     = "the step's summary so far\n"
		appended = "the audit ran\n"
	)

	path := filepath.Join(t.TempDir(), "step-summary.md")
	writeFile(t, path, held, 0o600)

	file, err := audit.OpenAppend(path, 0o600)
	if err != nil {
		t.Fatalf("audit.OpenAppend(%q) = %v, want the existing regular file's descriptor", path, err)
	}

	t.Cleanup(func() { _ = file.Close() })

	head := make([]byte, len(held))

	if _, err := file.ReadAt(head, 0); err != nil {
		t.Fatalf("read the descriptor audit.OpenAppend returned of %q at offset 0: %v, want the file's own bytes (issues #615, #614): an existing file command is opened O_RDWR, the open that never blocks on a named pipe swapped in between the Lstat and the open", path, err)
	}

	if want := held; string(head) != want {
		t.Errorf("the descriptor audit.OpenAppend returned of %q reads %q at offset 0, want %q: it is the file the Lstat saw, opened for reading and appending", path, head, want)
	}

	if _, err := file.WriteString(appended); err != nil {
		t.Fatalf("append to the descriptor audit.OpenAppend returned of %q: %v, want the run's summary line to reach the file", path, err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the file command %s: %v", path, err)
	}

	if want := held + appended; string(body) != want {
		t.Errorf("the file command %s holds %q, want %q: the descriptor appends what it holds and leaves the rest", path, body, want)
	}
}
