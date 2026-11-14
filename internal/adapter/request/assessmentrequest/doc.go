// Package assessmentrequest decodes the body of POST /v1/assessments, the HTTP
// API's request (PRD #103, ADR 0019). It is an adapter because the strict JSON
// decoder adapters share, jsondoc (ADR 0009), is internal to internal/adapter:
// the driving adapter internal/httpapi cannot import it, so the composition
// root passes Decode to httpapi as a function value, as it passes
// inventoryfile.FromBytes for the inventory itself.
//
// The body is untrusted input and is decoded only through jsondoc, never with
// encoding/json of its own. It must be one JSON object, valid UTF-8 without a
// byte order mark, with nothing after it but white space, holding the keys
// below, and Decode returns them as a Request:
//
//   - inventory: a JSON object, returned as its raw bytes, unchanged. This
//     package checks only that it is an object; the inventory reader
//     (inventoryfile.FromBytes) decodes it later, with its own rules (ADR 0006).
//   - targets: a non-empty array of strings, returned in order. Whether each
//     is a known target, repeated, empty or all with another key is the
//     caller's to check, with the CLI's rules.
//   - regions (optional, PRD #190): an object of strings mapping a target key
//     to the one region to assess that target in, at most MaxRegions members,
//     no key twice, no region empty (an empty one would mean every region).
//     Whether a key is a selected target and the region one the
//     target lists is the caller's to check. The keys are data, so an error
//     names a member by its index (regions[0]), never by its key.
//   - estimate (optional, PRD #190): a boolean, true when absent.
//
// inventory and targets are required. Malformed JSON, invalid UTF-8, a missing key, a value
// of the wrong JSON type (null included), an unknown key, a key repeated
// (also when it equals another ignoring case, as jsondoc refuses), an escaped
// lone surrogate and trailing data are refused, each with an error that wraps
// ErrDecode. A body over MaxBodySize is refused too.
//
// MaxBodySize is the inventory reader's cap (inventoryfile.MaxFileSize) plus
// 64 KiB for the envelope and the target list. An adapter imports no other
// adapter (depguard rule adapters-are-built-in-cmd), so the constant is
// written out here and a test pins it to that sum.
//
// Errors are one line and worded by jsondoc: they place a problem by line and
// column and may quote an escaped key or field name, never a value from the
// body. The package imports no net/http and does no I/O: it parses the bytes it
// is given.
package assessmentrequest
