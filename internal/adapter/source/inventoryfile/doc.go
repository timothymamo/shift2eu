// Package inventoryfile reads the inventory of a source from a JSON inventory
// file, the interchange format between a future `shift2eu discover` and
// `shift2eu assess` (ADR 0006). It implements the analysis.InventorySource port and
// passes the contract suite in sourcetest.
//
// New reads a file at a path. FromBytes reads the same format from bytes held
// in memory, such as the inventory the site's page hands its WebAssembly
// module (PRD #297): the same Source type, the same cap, decoder, rules and
// sentinels; only the way the bytes arrive differs, and its errors start with
// "inventory document" where a file's name the file. FromBytes takes ownership
// of its bytes, without a copy. Its errors never repeat a value from the
// document, which may come from anyone: where a file's error quotes a
// refused value, a FromBytes error names its position and field,
// resources[1].category, and wraps the same sentinel.
//
// The document is untrusted input. It is read only up to MaxFileSize bytes, then
// decoded through jsondoc, the strict JSON decoder adapters share (ADR 0009),
// which holds the rules this format shares with the target catalog snapshots:
// UTF-8 without a byte order mark; one JSON object with nothing after it; every
// key spelt exactly as the format defines it, character for character once
// JSON escapes are decoded, and never repeated; every value of the right JSON
// type; no string that escapes a lone surrogate; and the schema version check,
// with its wording. This package keeps the format's own decisions: its keys;
// schemaVersion 1 (ADR 0006) or 2 (ADR 0025), found before any key or type is
// checked (any other version, or none, is refused), with a version 2 key in a
// version 1 file refused as unknown; null read as an absent value for every
// field but resources, at every level of the version 2 objects (size, usage,
// monthlyCost) too, and refused as an element of resources, capabilities or
// relations; a version 2 number read as a float64 (a count as an integer
// written with no fraction or exponent), with one that overflows refused; a
// resources list (which may be empty) of at most MaxResources entries and at
// most MaxCapabilities capabilities and MaxRelations relations in each,
// counted as they are read, so a list is refused at its first element past the
// cap before that element is read; and its sentinels. The values are validated
// by the domain, through inventory.New: an unknown category is an error, never
// unclassified, and a version 2 field the domain refuses is named by its path.
//
// Errors are one line, name the file (if any) through text.QuotePath (which keeps the
// base name of a long path), and wrap ErrRead, ErrTooLarge, ErrDecode,
// ErrSchemaVersion, ErrTooManyResources, the domain's own sentinel or
// ctx.Err(). A problem in the document is placed by line and column, a
// malformed multi-byte character at its first byte, and a key or value is named
// by its path, resources[1].region. The text of OS and JSON decoder errors is
// never passed on: it would repeat the path or file contents unquoted.
//
// Cancelling the context ends a call at once, even while the file is opened or
// read: the file is opened and read on a goroutine of its own, closed on
// cancellation once it is open, which ends a read blocked on a pipe or FIFO. An
// open that blocks, such as a FIFO with no writer, or a read Go cannot poll,
// such as one on a stalled network file system, cannot be interrupted portably,
// so its goroutine waits until it returns or the process exits.
//
// Adapter layer: reads one local file or the bytes it is given, never the
// network or a cloud.
package inventoryfile
