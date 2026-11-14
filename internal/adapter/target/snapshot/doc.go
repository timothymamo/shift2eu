// Package snapshot reads a target catalog snapshot: the dated JSON document,
// schema version 2, that a target adapter embeds to describe what its target
// offers in the EU regions it declares (ADR 0007, ADR 0024). Every target
// adapter with an embedded snapshot reads it through Parse, so the format, its
// strictness and its errors are the same for every target and are pinned once,
// here.
//
// A snapshot holds the target's display name, the source URL and retrieval date
// (retrievedOn, YYYY-MM-DD) of its data, the EU regions it declares, its
// default region (the one estimates use when the user names none), and its
// offerings, each with a name, taxonomy categories, capabilities, regions,
// plans and storage prices, and optionally fixed fees and a plan billing cap. A
// plan has a name, vcpus, memoryGiB, storageGiB and prices; a price an amount,
// a currency and regions. Every field is required, at every level, and a list
// may be empty, with two exceptions. An offering's fixedFees (ADR 0026), the
// fixed monthly fee per instance (one Kubernetes cluster), may be left out,
// which means no fixed fee; when present it is a non-empty array of prices
// covering every region of the offering once, and an empty array or null is
// refused. An offering's planBillingCapHours (ADR 0029), the most hours a
// month the target bills one instance of one of its plans, whose prices are
// what that many hours cost, may be left out, which means uncapped; when
// present it is an integer from 1 to 744, and 0 or null is refused.
//
// Numbers are read from their JSON text, never through float64 (number.go):
// vcpus, storageGiB and planBillingCapHours are integers without a fraction or
// an exponent;
// memoryGiB and amount are exact decimals without an exponent, with at most 3
// and 4 fractional digits, stored as thousandths of a GiB and ten-thousandths
// of a euro. Zero with a minus sign and a value outside int64 are refused; any
// other negative value is read and left to catalog.New to refuse.
//
// Parse decodes a document through jsondoc, the strict JSON decoder adapters
// share (ADR 0009), which holds the rules of the first five items below, worded
// as it words them for the inventory file reader too. This package keeps the
// format's own decisions: its keys, every one required but fixedFees and
// planBillingCapHours; null refused
// everywhere; the size cap; the schema version it reads, 2; the number
// grammar; the checks after the walk; and its sentinels. Parse refuses a
// document, with an error, unless all of these hold, checked in this order:
//
//   - it is at most MaxDocumentBytes long, checked before anything else;
//   - it starts with no byte order mark, UTF-8 or UTF-16, which is refused by
//     name, and it is valid UTF-8, both checked before any decoding, since the
//     JSON decoder would read a byte that is not UTF-8 as U+FFFD;
//   - it is one JSON object with nothing after it but white space;
//   - its schemaVersion is the integer 2, found before any other key or value is
//     checked, so a document of another version is refused for its version
//     alone;
//   - read token by token: every key is spelt exactly as the format spells it,
//     character for character once JSON escapes are decoded, and appears once
//     per object; every field the format defines is present, not null, and of
//     its JSON type; every number follows the grammar above; and no string,
//     key or value, escapes a lone surrogate;
//   - its retrievedOn is a YYYY-MM-DD date;
//   - every declared region is non-blank, valid text (text.Validate), a region
//     code (ASCII letters and digits in groups joined by single hyphens, such
//     as fr-par, GRA or eu-central-1) and declared once, compared ignoring
//     ASCII case;
//   - its defaultRegion is a declared region, spelt as declared
//     (ErrUndeclaredDefaultRegion), so a document declaring no region cannot
//     be read;
//   - every offering region is a declared one, spelt as declared;
//   - catalog.New accepts its target, its source URL, its default region and
//     every offering's name, categories, capabilities, regions, plans, prices
//     fixed fees and plan billing cap. A price or fee region is checked
//     there, against its offering's regions, and so is that fixed fees cover
//     every region and that a plan billing cap is at most 744.
//
// The token walk is what encoding/json alone does not give: it matches keys
// without regard to case, keeps the last of a repeated key, reads null and a
// missing field as the zero value, and decodes an escaped lone surrogate to
// U+FFFD, so in each case the catalog would differ from the document without a
// word.
//
// Every error is one line and wraps ErrDecode, ErrSchemaVersion,
// ErrUndeclaredDefaultRegion, ErrUndeclaredRegion or the error of catalog.New.
// A problem the token walk finds, other than a missing top-level key, is placed
// by line and column (both from 1, the column in characters), and so are a byte
// that is not UTF-8, malformed JSON and data after the first JSON value. The
// other errors carry no position: they name the value by its path
// (offerings[1].regions[0]) or its name, or speak of the whole document. These
// are the size cap, a byte order mark, a document that is empty, cut short or
// not an object, the schema version, a missing top-level key, and the checks
// after the walk: retrievedOn, the declared regions, the default region, an
// undeclared offering region and catalog.New. Values from the document are
// shown through text.Quote, and the text of encoding/json and time.Parse errors
// is never passed on. Errors name neither the input nor any target: the adapter
// that embeds the document does.
//
// Parse takes the whole document in memory, and MaxDocumentBytes caps what it
// decodes, not what a caller reads. A caller that ever reads a snapshot from a
// stream, such as a fetched or user-supplied catalog (issue #13), must read at
// most MaxDocumentBytes+1 bytes (io.LimitReader) before calling Parse, so a
// larger one is still refused without being read whole. Such a fetching
// adapter must also meet the other preconditions ADR 0009 sets, under "Fetched
// catalogs", before it lands.
//
// Every snapshot sits beside a SOURCES.md that gives each offering a section of
// its own, naming the pages it comes from, the fact behind each category,
// capability and region, and the licence of each source, cites every plan name
// and price amount, fixed fees included (as Amount.String gives it), and an
// offering's plan billing cap as a code span, in its offering's section, and
// says in a Default region section which region is the default and why. This
// package's tests check those rules for every target adapter's catalog.json.
//
// Adapter layer: parses bytes it is given, opens no file and makes no network
// call.
package snapshot
