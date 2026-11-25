package github

// This is the package's one internal test. WriteArtifact's removal of the file
// it wrote when the write or the close fails (artifacts.go, doc.go) is
// observable only from inside the package: the file has to be handed to
// writeArtifact, the seam this test needs to fail a write or a close where no
// file system fails on demand, as ci/auditrun's writeResult is handed a file
// its own test can fail. The package's other tests stay black-box.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// artifactBody is the one JSON line WriteArtifact writes for a checked
// artifact, the bytes writeArtifact is handed here.
const artifactBody = `{"name":"checks-result","id":7,"digest":"sha256:` +
	"0000000000000000000000000000000000000000000000000000000000000000" +
	`"}` + "\n"

// TestArtifactWriteLeavesNoFileWhenTheWriteOrCloseFails pins artifacts.go's "a
// write or close that fails, which also removes the file, so a failed run
// writes none": what stays behind otherwise is a partial artifact.json the
// workflow's download step would read an ID and a digest from. The file
// writeArtifact is handed is this file's fake, which writes part of the body
// and then fails, or fails to close it, exactly as a full disk or a failing
// close would.
func TestArtifactWriteLeavesNoFileWhenTheWriteOrCloseFails(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		// after is how many bytes of the body the write takes before it fails:
		// the whole body for a close that fails.
		after    int
		closeErr error
	}{
		{name: "a write that fails after part of the body", after: 7},
		{name: "a close that fails", after: len(artifactBody), closeErr: errArtifactClose},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := filepath.Join(dir, ArtifactFile)

			file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, artifactFileMode)
			if err != nil {
				t.Fatalf("open the artifact file %s: %v", path, err)
			}

			err = writeArtifact(path, &artifactFailingFile{file: file, after: tc.after, closeErr: tc.closeErr}, []byte(artifactBody))
			if !errors.Is(err, publish.ErrConfig) {
				t.Errorf("writeArtifact(%s) = %v, want an error wrapping %v: a write or a close that fails fails the run, as a configuration error", path, err, publish.ErrConfig)
			}

			if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
				t.Errorf("the artifact file at %s is still there (stat: %v), want it removed: a write or a close that fails leaves no artifact.json, so the download step reads no partial ID or digest (artifacts.go)", path, statErr)
			}
		})
	}
}

// errArtifactWrite and errArtifactClose are the failures the fake artifact file
// answers: a write that stops after part of the body, and a close that fails.
var (
	errArtifactWrite = errors.New("the write failed after part of the body")
	errArtifactClose = errors.New("the close failed")
)

// artifactFailingFile is the open artifact file writeArtifact is handed, as a
// failing disk answers one: Write writes at most after bytes of the body and
// then fails, and Close fails when closeErr is set. It stands in for the
// *os.File WriteArtifact opens, so a test can fail the write or the close where
// no file system would fail on demand.
type artifactFailingFile struct {
	file     *os.File
	after    int
	closeErr error
}

// Write writes the first after bytes of body to the file and fails unless that
// is the whole body.
func (f *artifactFailingFile) Write(body []byte) (int, error) {
	n := min(f.after, len(body))

	if n > 0 {
		if _, err := f.file.Write(body[:n]); err != nil {
			return 0, err
		}
	}

	if n < len(body) {
		return n, errArtifactWrite
	}

	return n, nil
}

// Close closes the file, and fails when closeErr is set.
func (f *artifactFailingFile) Close() error {
	if f.closeErr != nil {
		_ = f.file.Close()

		return f.closeErr
	}

	return f.file.Close()
}
