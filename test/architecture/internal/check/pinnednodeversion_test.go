// Pins check.PinnedNodeVersion, the reader through which
// pienvdenyload_test.go names the Node dev/lima.yaml pins (issue #554): the
// line it reads, the form that line has, and every edit that would leave it
// reading no version at all.
//
// The function exists so that the load check and the workflows' read step agree
// on the one pin of the Node the VM installs for pi: both are built from
// nodeVersionPattern, which only matches a line of exactly two spaces,
// nodeVersion: , and a double-quoted value. The cases below hold the reader to
// that line and to the errors it returns otherwise, so a looser pattern (one
// that reads an unquoted value, another indent, or a CRLF file, none of which
// the workflows' sed reads) would fail here rather than install one Node and
// check for another.
package check_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/test/architecture/internal/check"
)

// limaNodeVersionPath is dev/lima.yaml, the document the workflows' read step
// and check.PinnedNodeVersion both read, by path from this package.
const limaNodeVersionPath = "../../../../dev/lima.yaml"

// limaNodeVersionRelease is the form the workflows' read step requires of what
// the sed read: one X.Y.Z release and nothing else.
var limaNodeVersionRelease = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// The dev/lima.yaml documents the cases break. Each is a param: block around
// the one nodeVersion line, written as dev/lima.yaml writes it: two spaces of
// indent and the version in double quotes.
const (
	// limaNodeVersionDoc is the fixture the cases that change one part of the
	// line start from, with other params before and after it.
	limaNodeVersionDoc = `minimumLimaVersion: "2.0.3"

param:
  nodeVersion: "24.11.1"
  goVersion: "1.25.5"
`

	// limaNoNodeVersionDoc pins no Node at all.
	limaNoNodeVersionDoc = `minimumLimaVersion: "2.0.3"

param:
  goVersion: "1.25.5"
`

	// limaEmptyNodeVersionDoc has the line, with nothing between the quotes: a
	// version actions/setup-node cannot install.
	limaEmptyNodeVersionDoc = `minimumLimaVersion: "2.0.3"

param:
  nodeVersion: ""
  goVersion: "1.25.5"
`

	// limaColumn0NodeVersionDoc and limaFourSpaceNodeVersionDoc write the line
	// at another indent, at column 0 and at four spaces: a YAML reader would
	// still read a version there, while the sed's two spaces read neither.
	limaColumn0NodeVersionDoc = `minimumLimaVersion: "2.0.3"

param:
nodeVersion: "24.11.1"
  goVersion: "1.25.5"
`

	limaFourSpaceNodeVersionDoc = `minimumLimaVersion: "2.0.3"

param:
    nodeVersion: "24.11.1"
  goVersion: "1.25.5"
`

	// limaUnquotedNodeVersionDoc writes the version without the double quotes,
	// which the sed does not read.
	limaUnquotedNodeVersionDoc = `minimumLimaVersion: "2.0.3"

param:
  nodeVersion: 24.11.1
  goVersion: "1.25.5"
`

	// limaTwoNodeVersionsDoc pins two versions: 24.11.1 first, 26.13.1 in a
	// second param: block.
	limaTwoNodeVersionsDoc = `minimumLimaVersion: "2.0.3"

param:
  nodeVersion: "24.11.1"
  goVersion: "1.25.5"

param2:
  nodeVersion: "26.13.1"
`
)

// TestPinnedNodeVersion pins what check.PinnedNodeVersion reads out of a
// dev/lima.yaml document: the version of the line nodeVersionPattern matches
// and nothing else, and an error naming what the document lacks for every
// document that holds no such line or pins no version in it (issue #554).
//
// The cases follow the workflows' sed, which reads the same pattern: a document
// the sed prints nothing for must error here, and a document it prints one
// version for must return that version. Where the two differ - two nodeVersion
// lines, which the sed prints both of and this reader returns the first of -
// the case pins what the reader does, so a change to it is a change here.
func TestPinnedNodeVersion(t *testing.T) {
	t.Parallel()

	lima, err := os.ReadFile(filepath.FromSlash(limaNodeVersionPath))
	if err != nil {
		t.Fatalf("read %s: %v", limaNodeVersionPath, err)
	}

	cases := []struct {
		name string
		// lima is the document to read; nil for dev/lima.yaml as it is.
		lima []byte
		// want is the version the document pins; "" when it pins none.
		want string
		// wantRelease says the returned version has to be one non-empty X.Y.Z
		// release: the case of dev/lima.yaml itself, whose Node a Renovate
		// update moves, so its version cannot be written here.
		wantRelease bool
		// wantErr is a part of the message of the error the document has to
		// produce; "" when it pins a version.
		wantErr string
	}{
		{name: "dev/lima.yaml as it is", wantRelease: true},
		{name: "a nodeVersion line among the param block's other params", lima: []byte(limaNodeVersionDoc), want: "24.11.1"},

		// CRLF line ends: the line's closing quote sits before a \r, not before
		// the \n, so the pattern's $ (Go's end of line in multi-line mode)
		// matches nowhere, and the document is refused like the sed's, which
		// prints nothing for the same file.
		{name: "CRLF line ends", lima: []byte(strings.ReplaceAll(limaNodeVersionDoc, "\n", "\r\n")), wantErr: "holds no line"},

		{name: "no nodeVersion line", lima: []byte(limaNoNodeVersionDoc), wantErr: "holds no line"},
		{name: "no lines at all", lima: []byte(""), wantErr: "holds no line"},
		{name: "an empty nodeVersion", lima: []byte(limaEmptyNodeVersionDoc), wantErr: "nodeVersion is empty"},
		{name: "the nodeVersion line at column 0", lima: []byte(limaColumn0NodeVersionDoc), wantErr: "holds no line"},
		{name: "the nodeVersion line at four spaces", lima: []byte(limaFourSpaceNodeVersionDoc), wantErr: "holds no line"},
		{name: "an unquoted nodeVersion", lima: []byte(limaUnquotedNodeVersionDoc), wantErr: "holds no line"},

		// Two nodeVersion lines: unlike the sed, which prints both and hands the
		// step's X.Y.Z check a two-line value that fails it, the reader returns
		// the first version silently.
		{name: "two nodeVersion lines", lima: []byte(limaTwoNodeVersionsDoc), want: "24.11.1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			doc := tc.lima
			if doc == nil {
				doc = lima
			}

			got, err := check.PinnedNodeVersion(doc)

			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("PinnedNodeVersion(%q) = %q, want an error holding %q: the workflows' sed reads no version there either (issue #554)", doc, got, tc.wantErr)
				}

				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("PinnedNodeVersion(%q) error = %q, want a message holding %q: the message names what the document lacks (issue #554)", doc, err, tc.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("PinnedNodeVersion(%q) error = %v, want the version the document pins (issue #554)", doc, err)
			}

			if tc.wantRelease && !limaNodeVersionRelease.MatchString(got) {
				t.Errorf("PinnedNodeVersion(%s) = %q, want one non-empty X.Y.Z release: the workflows' read step installs it and their check compares node against v%s (issue #548)", limaNodeVersionPath, got, got)
			}

			if tc.want != "" && got != tc.want {
				t.Errorf("PinnedNodeVersion(%q) = %q, want %q (issue #554)", doc, got, tc.want)
			}
		})
	}
}
