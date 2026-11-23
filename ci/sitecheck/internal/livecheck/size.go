package livecheck

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"mime"
	"strings"
)

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
	if maxBytes <= 0 {
		return Body{}, fmt.Errorf("a body cap of %d bytes, want a positive one", maxBytes)
	}

	encoding := strings.ToLower(strings.Trim(contentEncoding, " \t"))

	switch encoding {
	case "", EncodingIdentity:
		encoding = EncodingIdentity
	case EncodingGzip:
	default:
		return Body{}, fmt.Errorf("the Content-Encoding %s is neither gzip nor identity", Quote(contentEncoding))
	}

	raw, err := readCapped(r, maxBytes)
	if err != nil {
		return Body{}, fmt.Errorf("the body as transferred: %w", err)
	}

	body := Body{Encoding: encoding, Transferred: int64(len(raw)), Data: raw}

	if encoding == EncodingIdentity {
		return body, nil
	}

	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return Body{}, fmt.Errorf("the gzip body does not decode: %w", err)
	}

	defer func() { _ = zr.Close() }()

	body.Data, err = readCapped(zr, maxBytes)
	if err != nil {
		return Body{}, fmt.Errorf("the gzip body decoded: %w", err)
	}

	return body, nil
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
	size := Size{
		Measured:    true,
		Encoding:    b.Encoding,
		Transferred: b.Transferred,
		Decoded:     int64(len(b.Data)),
		ContentType: contentType,
	}

	var problems []string

	if mediaType, _, err := mime.ParseMediaType(contentType); err != nil || mediaType != "text/javascript" {
		problems = append(problems, fmt.Sprintf("the Content-Type %s is not text/javascript", Quote(contentType)))
	}

	if b.Encoding == EncodingGzip && size.Transferred > TransferBudget {
		problems = append(problems, fmt.Sprintf("%d bytes transferred gzip-compressed, over the budget of %d", size.Transferred, TransferBudget))
	}

	if size.Decoded > DecodedBudget {
		problems = append(problems, fmt.Sprintf("%d bytes decoded, over the budget of %d", size.Decoded, DecodedBudget))
	}

	if len(problems) > 0 {
		return size, errors.New(strings.Join(problems, "; "))
	}

	return size, nil
}
