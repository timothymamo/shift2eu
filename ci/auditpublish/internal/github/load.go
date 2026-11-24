package github

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// MaxPreviousReportBytes is the largest previous report LoadPrevious reads:
// a file over it is publish.ErrPreviousReport, never a truncated read.
const MaxPreviousReportBytes = 8 << 20

// errUnreadable is a file readRegular refuses: no regular file, another file
// than the one Lstat saw, over the cap, or unreadable. Its callers wrap their
// own class instead.
var errUnreadable = errors.New("unreadable")

// LoadResult reads the decrypted model result at path and parses it with the
// core (publish.ParseResult, commit the audited main commit). It refuses
// (publish.ErrConfig) a path that is not absolute and clean, and (
// publish.ErrResultInvalid) a path that names a symbolic link, no regular
// file, or a file over publish.MaxResultBytes, read capped at one byte more,
// never whole. The path is Lstat-ed before it is opened, and the opened file
// must be the file Lstat saw (os.SameFile), so a link swapped in between is
// refused too.
func LoadResult(path, commit string) (publish.Result, error) {
	if !absClean(path) {
		return publish.Result{}, fmt.Errorf("result: the path is not absolute and clean: %w", publish.ErrConfig)
	}

	data, err := readRegular(path, publish.MaxResultBytes)
	if err != nil {
		return publish.Result{}, fmt.Errorf("result: not one regular file under the cap: %w", publish.ErrResultInvalid)
	}

	return publish.ParseResult(data, commit)
}

// LoadPrevious reads the previous merged report from dir, the checkout's
// docs/security/audits: the newest name of its listing by
// publish.NewestReport, read capped at MaxPreviousReportBytes and parsed by
// publish.ParsePreviousReport. It returns nil and no error when no name of
// the listing is a report (the first run). It refuses (publish.ErrConfig) a
// dir that is not an absolute, clean, existing directory, and
// (publish.ErrPreviousReport) a newest report that is a symbolic link, a
// directory or any other non-regular file, one over the cap, one that cannot
// be read and one the core refuses. Only the newest report is opened.
func LoadPrevious(dir string) (*publish.Previous, error) {
	if !absClean(dir) {
		return nil, fmt.Errorf("previous report: the directory is not absolute and clean: %w", publish.ErrConfig)
	}

	if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("previous report: no directory: %w", publish.ErrConfig)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("previous report: the directory cannot be listed: %w", publish.ErrConfig)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}

	name, ok := publish.NewestReport(names)
	if !ok {
		return nil, nil
	}

	data, err := readRegular(filepath.Join(dir, name), MaxPreviousReportBytes)
	if err != nil {
		return nil, fmt.Errorf("previous report: not one regular file under the cap: %w", publish.ErrPreviousReport)
	}

	prev, err := publish.ParsePreviousReport(name, data)
	if err != nil {
		return nil, err
	}

	return &prev, nil
}

// absClean reports whether path is absolute and clean.
func absClean(path string) bool {
	return path != "" && filepath.IsAbs(path) && filepath.Clean(path) == path
}

// readRegular reads the regular file at path, at most limit bytes: a
// symbolic link, any other non-regular file, a file other than the one Lstat
// saw when it is opened, and a file over limit wrap errUnreadable.
func readRegular(path string, limit int64) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() {
		return nil, errUnreadable
	}

	//nolint:gosec // G304: an absolute, clean path the caller checked, Lstat-ed as one regular file just above
	f, err := os.Open(path)
	if err != nil {
		return nil, errUnreadable
	}

	defer func() { _ = f.Close() }()

	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) || !after.Mode().IsRegular() {
		return nil, errUnreadable
	}

	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errUnreadable
	}

	return data, nil
}
