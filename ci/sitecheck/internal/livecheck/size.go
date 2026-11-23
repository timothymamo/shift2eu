package livecheck

import "io"

// The module script's size budgets (behaviour 10, issue #346 item 2, ADR
// 0043).
const (
	// TransferBudget is the most bytes the edge may transfer for the module
	// script when it serves it gzip-compressed: 3 MiB.
	TransferBudget = 3 << 20
	// DecodedBudget is the most bytes the module script may have after
	// decoding, however it is served: 11 MiB.
	DecodedBudget = 11 << 20
)

// DefaultMaxBodyBytes is the cap on a response body's bytes, transferred and
// decoded alike, when Config.MaxBodyBytes is zero: 16 MiB, above
// DecodedBudget so an over-budget module script is measured, not cut off.
const DefaultMaxBodyBytes = 16 << 20

// The content encodings ReadBody knows.
const (
	EncodingIdentity = "identity"
	EncodingGzip     = "gzip"
)

// Body is a response body as read: what the edge transferred and what it
// decodes to.
type Body struct {
	// Encoding is EncodingIdentity (no Content-Encoding, or "identity") or
	// EncodingGzip.
	Encoding string
	// Transferred is the number of bytes of the body as transferred, before
	// content decoding.
	Transferred int64
	// Data is the body after content decoding.
	Data []byte
}

// ReadBody reads a response body from r, transferred with contentEncoding (the
// response's Content-Encoding header, "" when absent), and decodes it. It
// counts the bytes as transferred and decodes gzip; any other encoding, a
// list of encodings, or a gzip stream that does not decode is an error. More
// than maxBytes transferred bytes, or more than maxBytes decoded bytes, is an
// error, and ReadBody reads at most maxBytes+1 bytes of r, so an endless body
// cannot exhaust memory. maxBytes must be positive.
func ReadBody(contentEncoding string, r io.Reader, maxBytes int64) (Body, error) {
	return Body{}, stub(contentEncoding, r, maxBytes)
}

// Size is the module script's measurement (behaviour 10).
type Size struct {
	// Measured says whether the module script was fetched and read.
	Measured bool
	// Encoding is the Body's Encoding.
	Encoding string
	// Transferred and Decoded are the body's sizes as transferred and after
	// decoding.
	Transferred int64
	Decoded     int64
	// ContentType is the response's Content-Type; the report prints it only
	// through Quote.
	ContentType string
}

// MeasureModuleScript returns the Size of the module script's response, whose
// Content-Type is contentType and whose body is b, and nil when it meets
// behaviour 10: contentType is text/javascript, with parameters allowed
// (compared as a media type, without regard to case); when gzip-compressed,
// b.Transferred at most TransferBudget; always len(b.Data) at most
// DecodedBudget. Otherwise its error says which budget or the type failed.
// Served as identity, only the decoded budget applies.
func MeasureModuleScript(contentType string, b Body) (Size, error) {
	return Size{}, stub(contentType, b)
}
