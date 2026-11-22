// Pins the checksum step, the second of an audit run (ADR 0040's milestone-2
// amendment, issue #277): the sha512 of the tarball the install step kept must
// equal the npm integrity -claude-integrity names, and only then is the
// archive's one entry, package/claude, extracted to the state area's bin/ with
// mode 0500. A tarball that does not match, that is truncated, that carries an
// entry outside the binary or no binary at all is refused, and nothing outside
// that one file is written.
//
// The test writes the tarballs itself, so nothing here downloads, unpacks or
// runs a CLI.
package audit_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha512"
	"encoding/base64"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/timothymamo/shift2eu/ci/internal/audit"
)

// claudeEntry is the binary npm's tarball holds and the audit runs.
const claudeEntry = "package/claude"

// claudeScript is the body the fake binary is given: the tests never run it.
const claudeScript = "#!/bin/sh\necho this is the pinned Claude Code CLI\n"

// tarEntry is one entry of a test tarball.
type tarEntry struct {
	name     string
	body     string
	mode     int64
	typeflag byte
	linkname string
}

// writeTarball writes entries as a gzipped tar at dir/name and returns its path
// and its bytes, so a case can compute its integrity or hand over a damaged
// copy.
func writeTarball(t *testing.T, dir, name string, entries []tarEntry) (string, []byte) {
	t.Helper()

	var packed bytes.Buffer

	gzipWriter := gzip.NewWriter(&packed)
	tarWriter := tar.NewWriter(gzipWriter)

	for _, entry := range entries {
		flag := entry.typeflag
		if flag == 0 {
			flag = tar.TypeReg
		}

		mode := entry.mode
		if mode == 0 {
			mode = 0o755
		}

		header := &tar.Header{
			Name:     entry.name,
			Mode:     mode,
			Size:     int64(len(entry.body)),
			Typeflag: flag,
			Linkname: entry.linkname,
		}

		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatalf("write the tarball entry %s: %v", entry.name, err)
		}

		if flag == tar.TypeReg {
			if _, err := tarWriter.Write([]byte(entry.body)); err != nil {
				t.Fatalf("write the tarball entry %s: %v", entry.name, err)
			}
		}
	}

	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close the tarball: %v", err)
	}

	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("close the tarball's gzip stream: %v", err)
	}

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, packed.Bytes(), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}

	return path, packed.Bytes()
}

// integrityOf returns body's npm integrity: sha512- and the digest in base64,
// as the pin file writes it.
func integrityOf(body []byte) string {
	sum := sha512.Sum512(body)

	return "sha512-" + base64.StdEncoding.EncodeToString(sum[:])
}

// npmTarball is a tarball shaped like the one npm packs: the binary beside the
// package's own files, so a run that extracts anything but the binary shows it.
func npmTarball(binary string) []tarEntry {
	return []tarEntry{
		{name: "package/package.json", body: `{"name":"@anthropic-ai/claude-code-linux-x64"}`, mode: 0o644},
		{name: claudeEntry, body: binary, mode: 0o755},
		{name: "package/lib/index.js", body: "module.exports = {};\n", mode: 0o644},
	}
}

// TestInstallClaudeExtractsTheVerifiedBinary pins the run that passes: the
// archive's package/claude lands at the path the launcher runs it from, with
// mode 0500 and the archive's bytes, and the bin directory holds that one entry
// and nothing else.
func TestInstallClaudeExtractsTheVerifiedBinary(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	tarball, packed := writeTarball(t, root, "claude-code.tgz", npmTarball(claudeScript))

	dest := filepath.Join(root, "srv/audit/state/bin/claude")
	makeDir(t, filepath.Dir(dest))

	if err := audit.InstallClaude(tarball, integrityOf(packed), dest); err != nil {
		t.Fatalf("InstallClaude(%q) error = %v, want none: the integrity matches the tarball", tarball, err)
	}

	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read the extracted binary: %v", err)
	}

	if string(body) != claudeScript {
		t.Errorf("the extracted binary holds %q, want the tarball's package/claude, %q", body, claudeScript)
	}

	info, err := os.Lstat(dest)
	if err != nil {
		t.Fatalf("lstat the extracted binary: %v", err)
	}

	if got := info.Mode().Perm(); got != 0o500 {
		t.Errorf("the extracted binary's mode is %#o, want %#o: it is readable and runnable by the audit's user and by nobody else (ADR 0040)", got, 0o500)
	}

	entries, err := os.ReadDir(filepath.Dir(dest))
	if err != nil {
		t.Fatalf("read the bin directory: %v", err)
	}

	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}

	if want := []string{"claude"}; !slices.Equal(names, want) {
		t.Errorf("the bin directory holds %v, want %v: the step writes its one binary and nothing else", names, want)
	}
}

// TestInstallClaudeRefusesEveryOtherTarball pins the refusals: an integrity of
// another tarball, a truncated tarball, an integrity of the wrong shape, a
// tarball without package/claude, an archive that is no gzip, and an entry that
// would land outside the bin directory - a path with .., an absolute path, a
// symbolic link - leave no binary behind.
func TestInstallClaudeRefusesEveryOtherTarball(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// entries are the tarball's entries, or nil when the case damages the
		// bytes itself.
		entries []tarEntry
		// damage turns the tarball's bytes into the case's file, when set.
		damage func(t *testing.T, path string, packed []byte)
		// integrity replaces the tarball's own integrity when set.
		integrity string
		// wantMentioned is the text the refusal has to name, when the ADR names
		// it: the entry the archive lacks.
		wantMentioned string
	}{
		{
			name:      "an integrity of another tarball",
			entries:   npmTarball(claudeScript),
			integrity: integrityOf([]byte("another tarball")),
		},
		{
			name:    "a truncated tarball",
			entries: npmTarball(claudeScript),
			damage: func(t *testing.T, path string, packed []byte) {
				t.Helper()

				cut := packed[:len(packed)/2]
				if err := os.WriteFile(path, cut, 0o600); err != nil {
					t.Fatalf("truncate %s: %v", path, err)
				}
			},
		},
		{
			name:      "an integrity with no sha512 prefix",
			entries:   npmTarball(claudeScript),
			integrity: "0000000000000000000000000000000000000000000000000000000000000000000000000000000000000000==",
		},
		{
			name:      "an integrity of another algorithm",
			entries:   npmTarball(claudeScript),
			integrity: "sha1-0000000000000000000000000000000000000000",
		},
		{
			name:      "an integrity whose digest is no base64",
			entries:   npmTarball(claudeScript),
			integrity: "sha512-not a base64 digest",
		},
		{
			name:      "an integrity with nothing after the prefix",
			entries:   npmTarball(claudeScript),
			integrity: "sha512-",
		},
		{
			name:          "a tarball without the binary",
			entries:       []tarEntry{{name: "package/package.json", body: "{}", mode: 0o644}},
			wantMentioned: claudeEntry,
		},
		{
			name:    "a file that is no gzip and no tar",
			entries: nil,
			damage: func(t *testing.T, path string, packed []byte) {
				t.Helper()

				if err := os.WriteFile(path, []byte("this is no tarball at all\n"), 0o600); err != nil {
					t.Fatalf("overwrite %s: %v", path, err)
				}
			},
		},
		{
			name: "an entry whose path climbs out of the bin directory",
			entries: append(npmTarball(claudeScript), tarEntry{
				name: "package/../../escape", body: "escaped\n", mode: 0o644,
			}),
		},
		{
			name: "a symbolic link in place of the binary",
			entries: []tarEntry{
				{name: claudeEntry, typeflag: tar.TypeSymlink, linkname: "/bin/sh"},
			},
		},
		{
			name: "a symbolic link beside the binary",
			entries: append(npmTarball(claudeScript), tarEntry{
				name: "package/evil", typeflag: tar.TypeSymlink, linkname: "/bin/sh",
			}),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()

			var (
				tarball string
				packed  []byte
			)

			if tc.entries == nil {
				// The case overwrites the file itself, so it needs a tarball's
				// path and no bytes.
				tarball = filepath.Join(root, "claude-code.tgz")
				writeFile(t, tarball, "placeholder", 0o600)
			} else {
				tarball, packed = writeTarball(t, root, "claude-code.tgz", tc.entries)
			}

			if tc.damage != nil {
				tc.damage(t, tarball, packed)
			}

			integrity := tc.integrity
			if integrity == "" {
				damaged, err := os.ReadFile(tarball)
				if err != nil {
					t.Fatalf("read %s: %v", tarball, err)
				}

				integrity = integrityOf(damaged)
			}

			dest := filepath.Join(root, "srv/audit/state/bin/claude")
			makeDir(t, filepath.Dir(dest))

			err := audit.InstallClaude(tarball, integrity, dest)
			if err == nil {
				t.Fatalf("InstallClaude(%q, %q) = nil, want a refusal", tarball, integrity)
			}

			if tc.wantMentioned != "" && !bytes.Contains([]byte(err.Error()), []byte(tc.wantMentioned)) {
				t.Errorf("InstallClaude() error = %q, want it to name %q: the refusal says which entry the tarball lacks", err, tc.wantMentioned)
			}

			if _, statErr := os.Lstat(dest); !os.IsNotExist(statErr) {
				t.Errorf("a refused tarball left %s behind (%v); the binary is extracted only after the integrity matches", dest, statErr)
			}
		})
	}

	// An absolute entry: the tarball may write nothing outside the bin directory,
	// whatever path an entry names. The entry names a path inside the test's own
	// root, so a launcher that honoured it would be seen writing there.
	t.Run("an absolute entry", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		escape := filepath.Join(root, "absolute-escape")

		tarball, _ := writeTarball(t, root, "claude-code.tgz", append(npmTarball(claudeScript), tarEntry{
			name: escape, body: "escaped\n", mode: 0o644,
		}))

		packed, err := os.ReadFile(tarball)
		if err != nil {
			t.Fatalf("read %s: %v", tarball, err)
		}

		dest := filepath.Join(root, "srv/audit/state/bin/claude")
		makeDir(t, filepath.Dir(dest))

		if err := audit.InstallClaude(tarball, integrityOf(packed), dest); err == nil {
			t.Fatalf("InstallClaude(%q) = nil, want a refusal: an absolute entry names a path outside the bin directory", tarball)
		}

		if _, statErr := os.Lstat(escape); !os.IsNotExist(statErr) {
			t.Errorf("the tarball's absolute entry %s was written (%v); an archive entry outside the bin directory is refused", escape, statErr)
		}

		if _, statErr := os.Lstat(dest); !os.IsNotExist(statErr) {
			t.Errorf("a refused tarball left %s behind (%v)", dest, statErr)
		}
	})
}
