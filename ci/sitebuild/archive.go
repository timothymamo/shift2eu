package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// archiveTime is the one modification time of every archive entry: fixed, so
// the archive does not depend on the clock (PRD #297, Layer 3).
var archiveTime = time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)

// archiveMode is the one mode of every archive entry: readable, never
// executable.
const archiveMode = 0o644

// writeArchive writes the files, which are sorted by name, into a tar.gz at
// path: one regular entry per file, of one fixed time, owner and mode, in a
// gzip stream of no name and no modification time at the best compression.
func writeArchive(path string, files []bundleFile) error {
	var b bytes.Buffer

	compressed, err := gzip.NewWriterLevel(&b, gzip.BestCompression)
	if err != nil {
		return fmt.Errorf("compress the archive: %w", err)
	}

	archive := tar.NewWriter(compressed)

	for _, file := range files {
		header := &tar.Header{
			Typeflag: tar.TypeReg,
			Name:     file.name,
			Size:     int64(len(file.data)),
			Mode:     archiveMode,
			ModTime:  archiveTime,
			Format:   tar.FormatUSTAR,
		}

		if err := archive.WriteHeader(header); err != nil {
			return fmt.Errorf("archive %s: %w", file.name, err)
		}

		if _, err := archive.Write(file.data); err != nil {
			return fmt.Errorf("archive %s: %w", file.name, err)
		}
	}

	if err := archive.Close(); err != nil {
		return fmt.Errorf("close the archive: %w", err)
	}

	if err := compressed.Close(); err != nil {
		return fmt.Errorf("close the archive's compression: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create the archive's directory: %w", err)
	}

	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		return fmt.Errorf("write the archive: %w", err)
	}

	return nil
}
