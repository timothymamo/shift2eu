package livecheck

import "io"

// MaxRecordBytes caps how much of a record ParseHSTSRecord and
// ParsePreloadRecord read: a longer input is an error.
const MaxRecordBytes = 4 << 10

// RecordSchema is the one schema version of both records this program reads.
// The format is owned by PR A's hosting ADR (0042); until it lands this
// package reads the format the shared spec of PRD #297 milestone 3 fixes.
const RecordSchema = 1

// HSTSOutcome is site/records/hsts.json's "outcome".
type HSTSOutcome string

// The two outcomes of the HSTS record.
const (
	// HSTSMet: HSTS is in force; the host's header is held to the minimum.
	HSTSMet HSTSOutcome = "met"
	// HSTSWaived: the maintainer waived HSTS; the pages must say so.
	HSTSWaived HSTSOutcome = "waived"
)

// PlainHTTPWWW is site/records/hsts.json's "plain_http_www": what
// http://<host>/ does.
type PlainHTTPWWW string

// The two answers of plain HTTP on the canonical host.
const (
	// PlainHTTPRedirect: it redirects to https://<host>/<p> like the apex.
	PlainHTTPRedirect PlainHTTPWWW = "redirect"
	// PlainHTTPRefuseOrClose: it refuses or closes the connection
	// (behaviour 6's exception).
	PlainHTTPRefuseOrClose PlainHTTPWWW = "refuse-or-close"
)

// HSTSRecord is site/records/hsts.json:
//
//	{"schema": 1, "outcome": "met"|"waived", "preload_pursued": bool, "plain_http_www": "redirect"|"refuse-or-close"}
type HSTSRecord struct {
	Outcome        HSTSOutcome
	PreloadPursued bool
	PlainHTTPWWW   PlainHTTPWWW
}

// PreloadRecord is site/records/preload.json:
//
//	{"schema": 1, "on_preload_list": bool}
type PreloadRecord struct {
	OnPreloadList bool
}

// ParseHSTSRecord reads hsts.json strictly from r, at most MaxRecordBytes
// (more is an error): exactly one JSON object with exactly the four fields
// above, each present, of its type (a JSON null is not a value), "schema"
// equal to RecordSchema, "outcome" and "plain_http_www" one of their
// constants, nothing after the object but white space.
func ParseHSTSRecord(r io.Reader) (HSTSRecord, error) {
	return HSTSRecord{}, stub(r)
}

// ParsePreloadRecord reads preload.json strictly from r, under the same rules
// as ParseHSTSRecord: exactly the fields "schema" (RecordSchema) and
// "on_preload_list" (a boolean).
func ParsePreloadRecord(r io.Reader) (PreloadRecord, error) {
	return PreloadRecord{}, stub(r)
}

// Records are both records as the caller read them. A record whose file is
// missing or unreadable, or which its parser refused, has its error set and
// its value ignored: Run then fails safe (behaviour 8). An HSTS record with
// an error fails a KindHSTS check (it never counts as waived or met). A preload
// record with an error counts as not on the preload list, so privacy.html must
// carry the downgrade-risk statement, and also fails a KindHSTS check of its
// own, so an unreadable record never passes silently.
type Records struct {
	HSTS       HSTSRecord
	HSTSErr    error
	Preload    PreloadRecord
	PreloadErr error
}
