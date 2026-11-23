package github

import "github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"

// MaxPreviousReportBytes is the largest previous report LoadPrevious reads:
// a file over it is publish.ErrPreviousReport, never a truncated read.
const MaxPreviousReportBytes = 8 << 20

// LoadResult reads the decrypted model result at path and parses it with the
// core (publish.ParseResult, commit the audited main commit). It refuses
// (publish.ErrConfig) a path that is not absolute and clean, and (
// publish.ErrResultInvalid) a path that names a symbolic link, no regular
// file, or a file over publish.MaxResultBytes, read capped at one byte more,
// never whole. The path is Lstat-ed before it is opened, and the opened file
// must be the file Lstat saw (os.SameFile), so a link swapped in between is
// refused too.
func LoadResult(_, _ string) (publish.Result, error) {
	return publish.Result{}, errNotImplemented
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
func LoadPrevious(_ string) (*publish.Previous, error) {
	return nil, errNotImplemented
}
