package audit

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// integrityPrefix starts an npm integrity of the one algorithm the pin uses.
const integrityPrefix = "sha512-"

// claudeEntry is the one entry of the tarball the audit runs.
const claudeEntry = "package/claude"

// The caps on what InstallClaude reads: the tarball as kept, and the binary
// it extracts. The pinned CLI's tarball is a fraction of either.
const (
	maxTarball = 1 << 30
	maxBinary  = 1 << 30
)

// binaryMode is the extracted CLI's mode: readable and runnable by the
// audit's user alone.
const binaryMode os.FileMode = 0o500

// errTarball is a tarball the checksum step refuses.
var errTarball = errors.New("the Claude Code tarball is refused")

// ParseIntegrity returns the sha512 digest an npm integrity names: the prefix
// sha512- and the digest's 64 bytes in standard base64, nothing else.
func ParseIntegrity(integrity string) ([]byte, error) {
	encoded, ok := strings.CutPrefix(integrity, integrityPrefix)
	if !ok {
		return nil, fmt.Errorf("%w: the integrity is no %s<base64> of the tarball", errTarball, integrityPrefix)
	}

	digest, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(digest) != sha512.Size {
		return nil, fmt.Errorf("%w: the integrity's digest is no base64 of %d bytes", errTarball, sha512.Size)
	}

	return digest, nil
}

// InstallClaude checks the tarball against integrity and only then writes its
// one entry package/claude to bin, mode 0500. The tarball is read once, so the
// bytes extracted are the bytes checked. Every entry is checked before
// anything is written: an entry that is no regular file or directory, a path
// that is absolute or climbs out, a second binary or none at all refuses the
// tarball, and a refused tarball leaves no binary behind.
func InstallClaude(tarball, integrity, bin string) error {
	want, err := ParseIntegrity(integrity)
	if err != nil {
		return err
	}

	packed, err := readCapped(tarball, maxTarball)
	if err != nil {
		return fmt.Errorf("%w: %w", errTarball, err)
	}

	got := sha512.Sum512(packed)
	if subtle.ConstantTimeCompare(got[:], want) != 1 {
		return fmt.Errorf("%w: its sha512 does not match the pin's integrity", errTarball)
	}

	binary, err := claudeBinary(packed)
	if err != nil {
		return err
	}

	return writeBinary(bin, binary)
}

// readCapped reads the file at name, refusing one over limit bytes.
func readCapped(name string, limit int64) ([]byte, error) {
	file, err := os.Open(name) //nolint:gosec // G304: a path of the launcher's own layout or the install step's tarball, whose bytes are checked before use
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer file.Close()

	body, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}

	if int64(len(body)) > limit {
		return nil, fmt.Errorf("%s is over %d bytes", name, limit)
	}

	return body, nil
}

// claudeBinary returns the body of the tarball's package/claude, checking
// every entry of the archive first.
func claudeBinary(packed []byte) ([]byte, error) {
	unzipped, err := gzip.NewReader(bytes.NewReader(packed))
	if err != nil {
		return nil, fmt.Errorf("%w: it is no gzip stream: %w", errTarball, err)
	}

	archive := tar.NewReader(unzipped)

	var binary []byte

	found := false

	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("%w: read its entries: %w", errTarball, err)
		}

		if err := checkEntry(header); err != nil {
			return nil, err
		}

		if header.Name != claudeEntry {
			continue
		}

		if found {
			return nil, fmt.Errorf("%w: it holds %s twice", errTarball, claudeEntry)
		}

		found = true

		binary, err = io.ReadAll(io.LimitReader(archive, maxBinary+1))
		if err != nil {
			return nil, fmt.Errorf("%w: read %s: %w", errTarball, claudeEntry, err)
		}

		if len(binary) > maxBinary {
			return nil, fmt.Errorf("%w: %s is over %d bytes", errTarball, claudeEntry, maxBinary)
		}
	}

	// The tar reader stops at the archive's end blocks; reading the gzip
	// stream to its end checks its own trailer.
	if _, err := io.Copy(io.Discard, io.LimitReader(unzipped, maxTarball)); err != nil {
		return nil, fmt.Errorf("%w: read its gzip stream: %w", errTarball, err)
	}

	if !found {
		return nil, fmt.Errorf("%w: it holds no %s", errTarball, claudeEntry)
	}

	return binary, nil
}

// checkEntry refuses an entry that is no regular file or directory, or whose
// path is not a clean relative path under package/: nothing of the archive
// may point outside it, even an entry the step does not extract.
func checkEntry(header *tar.Header) error {
	switch header.Typeflag {
	case tar.TypeReg, tar.TypeDir:
	default:
		return fmt.Errorf("%w: the entry %q is no regular file or directory", errTarball, header.Name)
	}

	name := strings.TrimSuffix(header.Name, "/")
	if path.IsAbs(name) || strings.Contains(name, `\`) || path.Clean(name) != name ||
		(name != "package" && !strings.HasPrefix(name, "package/")) {
		return fmt.Errorf("%w: the entry %q is no clean path under package/", errTarball, header.Name)
	}

	if header.Name == claudeEntry && header.Typeflag != tar.TypeReg {
		return fmt.Errorf("%w: %s is no regular file", errTarball, claudeEntry)
	}

	return nil
}

// writeBinary writes body to a new file beside bin and renames it there, so
// bin is never a half-written binary.
func writeBinary(bin string, body []byte) error {
	file, err := os.CreateTemp(filepath.Dir(bin), ".claude-*")
	if err != nil {
		return fmt.Errorf("write the Claude Code binary: %w", err)
	}

	name := file.Name()

	_, err = file.Write(body)
	if err == nil {
		err = file.Chmod(binaryMode)
	}

	if closeErr := file.Close(); err == nil {
		err = closeErr
	}

	if err == nil {
		err = os.Rename(name, bin)
	}

	if err != nil {
		_ = os.Remove(name)

		return fmt.Errorf("write the Claude Code binary: %w", err)
	}

	return nil
}
