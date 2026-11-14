package jsondoc_test

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/internal/adapter/internal/jsondoc"
	"github.com/timothymamo/shift2eu/internal/domain/text"
)

// errInvalid stands in for a format's own sentinel for a document it cannot
// read: every error about the document must wrap it.
var errInvalid = errors.New("invalid document")

// errElementRefused stands in for an error a format's own element callback
// returns, such as one that refuses an array past a cap: it is about the
// format's policy, not about the document.
var errElementRefused = errors.New("element refused")

// maxDocumentBytes is the cap the tests open documents with: room for every
// fixture here, so only the size-cap cases reach the cap.
const maxDocumentBytes = 1 << 20

// open returns data as a Document, failing the test if Open refuses it.
func open(t *testing.T, data string) jsondoc.Document {
	t.Helper()

	doc, err := jsondoc.Open([]byte(data), maxDocumentBytes, errInvalid)
	if err != nil {
		t.Fatalf("Open() of %s returned unexpected error: %v", text.Quote(data), err)
	}

	return doc
}

// checkDocumentError pins what every error about a document owes the caller,
// whatever the problem: it wraps the format's sentinel, it is one line, and it
// never passes on the wording of encoding/json, which the format's errors must
// not adopt.
func checkDocumentError(t *testing.T, err error) {
	t.Helper()

	if err == nil {
		t.Fatal("the call returned no error, want one")
	}

	if !errors.Is(err, errInvalid) {
		t.Errorf("error = %v, want one wrapping the format's sentinel %v", err, errInvalid)
	}

	msg := err.Error()
	if strings.ContainsAny(msg, "\r\n") {
		t.Errorf("error = %q, want a single line", msg)
	}

	for _, unwanted := range []string{"invalid character", "cannot unmarshal", "unexpected end of JSON input"} {
		if strings.Contains(msg, unwanted) {
			t.Errorf("error = %q, want it without encoding/json's wording %s", msg, text.Quote(unwanted))
		}
	}
}

// readRaw drives the walk over a document object whose keys are keys, reading
// every value whole, and returns the first error: Open's or the walk's.
func readRaw(doc jsondoc.Document, keys []jsondoc.Key) error {
	w := doc.Walker()

	return w.Object(keys, func(string) error {
		_, err := w.Raw()

		return err
	})
}

// TestOpen pins the checks Open makes on the document as a whole, in the order
// it makes them: the size cap, a byte order mark refused by name, valid UTF-8,
// one JSON value with nothing after it, and that value an object. A whole-
// document error names no position; a document that is not UTF-8 or not valid
// JSON is placed by line and column, in characters, at the first byte of a
// malformed multi-byte character.
func TestOpen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
		// maxBytes is the cap data is opened with; 0 means maxDocumentBytes.
		maxBytes int
		// wantAccepted is whether Open accepts the document.
		wantAccepted bool
		// wantContains lists substrings the message must hold and wantAbsent
		// ones it must not.
		wantContains []string
		wantAbsent   []string
	}{
		{
			name:         "exactly the size cap",
			data:         "{}",
			maxBytes:     len("{}"),
			wantAccepted: true,
		},
		{
			name:         "one byte over the size cap",
			data:         "{}",
			maxBytes:     len("{}") - 1,
			wantContains: []string{"more than the limit"},
			wantAbsent:   []string{"line "},
		},
		{
			name:         "UTF-8 byte order mark",
			data:         "\xef\xbb\xbf{}",
			wantContains: []string{"byte order mark"},
			wantAbsent:   []string{"line "},
		},
		{
			name:         "UTF-16 little endian byte order mark",
			data:         "\xff\xfe{\x00}\x00",
			wantContains: []string{"byte order mark"},
			wantAbsent:   []string{"line "},
		},
		{
			name:         "UTF-16 big endian byte order mark",
			data:         "\xfe\xff\x00{\x00}",
			wantContains: []string{"byte order mark"},
			wantAbsent:   []string{"line "},
		},
		{
			name:         "a byte that is not UTF-8",
			data:         "{\n  \"a\": \xe9\n}",
			wantContains: []string{"not valid UTF-8", "line 2, column 8"},
		},
		{
			name:         "empty document",
			data:         "",
			wantContains: []string{"the document is empty"},
			wantAbsent:   []string{"line "},
		},
		{
			name:         "white space only",
			data:         " \n\t",
			wantContains: []string{"the document is empty"},
			wantAbsent:   []string{"line "},
		},
		{
			name:         "document cut short",
			data:         `{"a": 1`,
			wantContains: []string{"ends in the middle of a JSON value"},
			wantAbsent:   []string{"line "},
		},
		{
			name:         "array at the top level",
			data:         `[1, 2]`,
			wantContains: []string{"not a JSON object", "an array"},
			wantAbsent:   []string{"line "},
		},
		{
			name:         "string at the top level",
			data:         `"text"`,
			wantContains: []string{"not a JSON object", "a string"},
			wantAbsent:   []string{"line "},
		},
		{
			name:         "null at the top level",
			data:         `null`,
			wantContains: []string{"not a JSON object", "null"},
			wantAbsent:   []string{"line "},
		},
		{
			name:         "data after the first JSON value",
			data:         `{} {}`,
			wantContains: []string{"unexpected data after the first JSON value", "line 1, column 4"},
		},
		{
			name:         "malformed JSON",
			data:         `{"a": }`,
			wantContains: []string{"malformed JSON", "line 1, column 7"},
		},
		{
			// encoding/json's syntax offset counts every byte of the character
			// it refused, so a malformed multi-byte character must be placed at
			// its first byte: é starts at column 2 of line 3, not at column 3.
			name:         "malformed multi-byte character at its first byte",
			data:         "{\n \"a\": 1,\n é\n}",
			wantContains: []string{"malformed JSON", "line 3, column 2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			maxBytes := tt.maxBytes
			if maxBytes == 0 {
				maxBytes = maxDocumentBytes
			}

			_, err := jsondoc.Open([]byte(tt.data), maxBytes, errInvalid)
			if tt.wantAccepted {
				if err != nil {
					t.Fatalf("Open() of %s returned unexpected error: %v", text.Quote(tt.data), err)
				}

				return
			}

			checkDocumentError(t, err)

			msg := err.Error()
			for _, want := range tt.wantContains {
				if !strings.Contains(msg, want) {
					t.Errorf("Open() error = %q, want it to contain %s", msg, text.Quote(want))
				}
			}

			for _, unwanted := range tt.wantAbsent {
				if strings.Contains(msg, unwanted) {
					t.Errorf("Open() error = %q, want it without %s", msg, text.Quote(unwanted))
				}
			}
		})
	}
}

// TestLookup pins Document.Lookup: the JSON text of the first top-level key
// spelt exactly, as the document spells the value, and the first key before it
// that equals key ignoring case, so a format can name the key that was meant.
// It reads only the object's keys and steps over every other value.
func TestLookup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
		key  string
		// wantRaw is the value's JSON text, or "" for no such key.
		wantRaw string
		// wantFolded is the first case variant before the match, or "".
		wantFolded string
	}{
		{name: "exact key", data: `{"name": 1}`, key: "name", wantRaw: "1"},
		{name: "string value keeps its quotes", data: `{"name": "1"}`, key: "name", wantRaw: `"1"`},
		{name: "object value", data: `{"name": {"a": [1, 2]}}`, key: "name", wantRaw: `{"a": [1, 2]}`},
		{name: "missing key", data: `{"other": 1}`, key: "name"},
		{name: "case variant", data: `{"Name": 1}`, key: "name", wantFolded: "Name"},
		{
			name:       "case variant before the exact key",
			data:       `{"nAmE": 1, "name": 2}`,
			key:        "name",
			wantRaw:    "2",
			wantFolded: "nAmE",
		},
		{
			name:    "case variant after the exact key",
			data:    `{"name": 1, "nAmE": 2}`,
			key:     "name",
			wantRaw: "1",
		},
		{
			name:    "key spelt with a JSON escape",
			data:    `{"na\u006de": 1}`,
			key:     "name",
			wantRaw: "1",
		},
		{
			name:    "steps over nested values",
			data:    `{"a": [1, {"b": 2}], "name": 3}`,
			key:     "name",
			wantRaw: "3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			raw, folded, err := open(t, tt.data).Lookup(tt.key)
			if err != nil {
				t.Fatalf("Lookup(%s) of %s returned unexpected error: %v", text.Quote(tt.key), text.Quote(tt.data), err)
			}

			if got := string(raw); got != tt.wantRaw {
				t.Errorf("Lookup(%s) raw = %s, want %s", text.Quote(tt.key), text.Quote(got), text.Quote(tt.wantRaw))
			}

			if tt.wantRaw == "" && raw != nil {
				t.Errorf("Lookup(%s) raw = %v, want nil for a missing key", text.Quote(tt.key), raw)
			}

			if folded != tt.wantFolded {
				t.Errorf("Lookup(%s) folded = %s, want %s", text.Quote(tt.key), text.Quote(folded), text.Quote(tt.wantFolded))
			}
		})
	}
}

// TestObjectKeys pins Walker.Object's key rules: a key must be character for
// character one the format lists, once JSON escapes are decoded, and the object
// may not repeat a key or lack a Required one. A hint names the listed key when
// the document's key equals one ignoring case, and a document value reaches the
// message only through text.Quote.
func TestObjectKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
		keys []jsondoc.Key
		// wantAccepted is whether the object is read.
		wantAccepted bool
		// wantContains lists substrings the message must hold and wantAbsent
		// ones it must not.
		wantContains []string
		wantAbsent   []string
		// wantQuoted lists document values the message must show through
		// text.Quote.
		wantQuoted []string
	}{
		{
			name:         "listed key",
			data:         `{"name": 1}`,
			keys:         []jsondoc.Key{{Name: "name"}},
			wantAccepted: true,
		},
		{
			name:         "optional key that is missing",
			data:         `{}`,
			keys:         []jsondoc.Key{{Name: "name"}},
			wantAccepted: true,
		},
		{
			name:         "listed key spelt with a JSON escape",
			data:         `{"na\u006de": 1}`,
			keys:         []jsondoc.Key{{Name: "name"}},
			wantAccepted: true,
		},
		{
			name:         "unknown key",
			data:         `{"bogus": 1}`,
			keys:         []jsondoc.Key{{Name: "name"}},
			wantContains: []string{"unknown key"},
			wantQuoted:   []string{"bogus"},
		},
		{
			name:         "key differing only in case",
			data:         `{"Name": 1}`,
			keys:         []jsondoc.Key{{Name: "name"}},
			wantContains: []string{"unknown key", "keys are exact", "did you mean"},
			wantQuoted:   []string{"Name", "name"},
		},
		{
			// The escape is a way of spelling a key, not a way past the match:
			// the message names the key the escape decodes to, not the escape.
			name:         "escape that decodes to a different key",
			data:         `{"nam\u0065s": 1}`,
			keys:         []jsondoc.Key{{Name: "name"}},
			wantContains: []string{"unknown key"},
			wantQuoted:   []string{"names"},
			wantAbsent:   []string{`\u0065`},
		},
		{
			name:         "repeated key",
			data:         `{"name": 1, "name": 2}`,
			keys:         []jsondoc.Key{{Name: "name"}},
			wantContains: []string{"repeated key"},
			wantQuoted:   []string{"name"},
		},
		{
			name:         "missing required key",
			data:         `{}`,
			keys:         []jsondoc.Key{{Name: "name", Required: true}},
			wantContains: []string{"required key", "missing"},
			wantQuoted:   []string{"name"},
		},
		{
			name:         "unknown key is shown through text.Quote",
			data:         `{"a\nb\"c": 1}`,
			keys:         []jsondoc.Key{{Name: "name"}},
			wantContains: []string{"unknown key"},
			wantQuoted:   []string{"a\nb\"c"},
		},
		{
			name:         "key that escapes a lone surrogate",
			data:         `{"\ud800": 1}`,
			keys:         []jsondoc.Key{{Name: "name"}},
			wantContains: []string{"U+D800", "surrogate on its own"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			doc := open(t, tt.data)
			err := readRaw(doc, tt.keys)
			if tt.wantAccepted {
				if err != nil {
					t.Fatalf("Object() of %s with the keys %v returned unexpected error: %v",
						text.Quote(tt.data), tt.keys, err)
				}

				return
			}

			checkDocumentError(t, err)

			msg := err.Error()
			for _, want := range tt.wantContains {
				if !strings.Contains(msg, want) {
					t.Errorf("Object() error = %q, want it to contain %s", msg, text.Quote(want))
				}
			}

			for _, unwanted := range tt.wantAbsent {
				if strings.Contains(msg, unwanted) {
					t.Errorf("Object() error = %q, want it without %s", msg, text.Quote(unwanted))
				}
			}

			for _, value := range tt.wantQuoted {
				if quoted := text.Quote(value); !strings.Contains(msg, quoted) {
					t.Errorf("Object() error = %q, want it to show %s through text.Quote", msg, quoted)
				}
			}
		})
	}
}

// TestObjectKeyCount pins the cap on the keys one kind of object may list: a
// format that lists more than MaxKeys keys has made a mistake in its use of the
// package, not the document one, so the error starts with "jsondoc:", wraps
// jsondoc.ErrMisuse and wraps no format sentinel.
func TestObjectKeyCount(t *testing.T) {
	t.Parallel()

	keys := func(n int) []jsondoc.Key {
		listed := make([]jsondoc.Key, n)
		for i := range listed {
			listed[i] = jsondoc.Key{Name: "k" + strconv.Itoa(i)}
		}

		return listed
	}

	doc := open(t, "{}")

	if err := doc.Walker().Object(keys(jsondoc.MaxKeys), func(string) error { return nil }); err != nil {
		t.Errorf("Object() with MaxKeys = %d keys returned unexpected error: %v", jsondoc.MaxKeys, err)
	}

	err := doc.Walker().Object(keys(jsondoc.MaxKeys+1), func(string) error { return nil })

	checkMisuseError(t, err)

	msg := err.Error()
	if want := strconv.Itoa(jsondoc.MaxKeys); !strings.Contains(msg, want) {
		t.Errorf("Object() error = %q, want it to name the limit %d", msg, jsondoc.MaxKeys)
	}
}

// checkMisuseError pins what every misuse of the package owes the caller: it
// reports a mistake in the format's use of the package, not in the document, so
// it keeps the "jsondoc:" prefix, wraps jsondoc.ErrMisuse and never wraps the
// format's own sentinel.
func checkMisuseError(t *testing.T, err error) {
	t.Helper()

	if err == nil {
		t.Fatal("the call returned no error, want one reporting the misuse of the package")
	}

	if !errors.Is(err, jsondoc.ErrMisuse) {
		t.Errorf("error = %v, want one wrapping jsondoc.ErrMisuse", err)
	}

	if errors.Is(err, errInvalid) {
		t.Errorf("error = %v, want it not to wrap the format's sentinel %v: the document is not at fault", err, errInvalid)
	}

	msg := err.Error()
	if !strings.HasPrefix(msg, "jsondoc:") {
		t.Errorf("error = %q, want it to start with %q, a mistake in the format's use of the package", msg, "jsondoc:")
	}

	if strings.ContainsAny(msg, "\r\n") {
		t.Errorf("error = %q, want a single line", msg)
	}

	if strings.Contains(msg, "%!w(<nil>)") {
		t.Errorf("error = %q, want it without a sentinel that failed to format", msg)
	}
}

// TestMisuse pins that a mistake in a format's use of the package is reported as
// such, and never blamed on the document: a callback that returns without
// reading the value it was called for, or reads it twice, a keys list that holds
// the same Name twice, and a zero Document, whose Lookup and Walker have no
// document to read.
func TestMisuse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// misuse drives the walk the wrong way and returns the error it reports.
		misuse func(t *testing.T) error
		// wantAbsent lists substrings the message must not contain: a misuse is
		// never the document's fault.
		wantAbsent []string
		// wantNotWrapped lists errors the message must not wrap either, such as
		// one a callback returned of its own.
		wantNotWrapped []error
	}{
		{
			name: "object value callback that reads nothing",
			misuse: func(t *testing.T) error {
				t.Helper()

				return open(t, `{"x": 1}`).Walker().Object([]jsondoc.Key{{Name: "x"}}, func(string) error { return nil })
			},
		},
		{
			name: "array element callback that reads nothing",
			misuse: func(t *testing.T) error {
				t.Helper()

				w := open(t, `{"s": ["x"]}`).Walker()

				return w.Object([]jsondoc.Key{{Name: "s"}}, func(string) error {
					return w.Array("an array", func(int) error { return nil })
				})
			},
		},
		{
			// The second String() reads the object's next key as a value, and the
			// walk then reports that key as unknown: the mistake is the callback's.
			name: "object value callback that reads twice",
			misuse: func(t *testing.T) error {
				t.Helper()

				w := open(t, `{"x": "a", "y": "b"}`).Walker()

				return w.Object([]jsondoc.Key{{Name: "x"}, {Name: "y"}}, func(string) error {
					if _, err := w.String(); err != nil {
						return err
					}

					_, err := w.String()

					return err
				})
			},
			wantAbsent: []string{"unknown key"},
		},
		{
			// The second String() reads the next element as a value, and the array
			// walk then steps over the element after it: an element goes unread
			// without a word.
			name: "array element callback that reads twice",
			misuse: func(t *testing.T) error {
				t.Helper()

				w := open(t, `{"s": ["x", "y", "z"]}`).Walker()

				return w.Object([]jsondoc.Key{{Name: "s"}}, func(string) error {
					return w.Array("an array", func(int) error {
						if _, err := w.String(); err != nil {
							return err
						}

						_, err := w.String()

						return err
					})
				})
			},
		},
		{
			// The same Name twice cannot say which of the two keys an object key
			// is: today the second is never marked seen, so a required duplicate is
			// reported as a key the document lacks.
			name: "keys list that holds the same Name twice",
			misuse: func(t *testing.T) error {
				t.Helper()

				keys := []jsondoc.Key{{Name: "a", Required: true}, {Name: "a", Required: true}}
				w := open(t, `{"a": 1}`).Walker()

				return w.Object(keys, func(string) error {
					_, err := w.Raw()

					return err
				})
			},
			wantAbsent: []string{"required key", "missing"},
		},
		{
			name: "Lookup on the zero Document",
			misuse: func(t *testing.T) error {
				t.Helper()

				var doc jsondoc.Document

				_, _, err := doc.Lookup("schemaVersion")

				return err
			},
		},
		{
			name: "first read on the zero Document's Walker",
			misuse: func(t *testing.T) error {
				t.Helper()

				var doc jsondoc.Document

				w := doc.Walker()

				return w.Object([]jsondoc.Key{{Name: "schemaVersion"}}, func(string) error {
					_, err := w.Raw()

					return err
				})
			},
		},
		{
			// Every error Open returns wraps the format's sentinel, so a nil one
			// would leave the caller nothing to recognise the document's problem by.
			name: "Open without the format's sentinel",
			misuse: func(t *testing.T) error {
				t.Helper()

				_, err := jsondoc.Open([]byte(`{}`), maxDocumentBytes, nil)

				return err
			},
		},
		{
			name: "Document.Version without the format's sentinel",
			misuse: func(t *testing.T) error {
				t.Helper()

				return open(t, `{"v": 1}`).Version("v", 1, nil)
			},
		},
		{
			name: "Walker.Version without the format's sentinel",
			misuse: func(t *testing.T) error {
				t.Helper()

				w := open(t, `{"v": 1}`).Walker()

				return w.Object([]jsondoc.Key{{Name: "v"}}, func(string) error {
					return w.Version(1, nil)
				})
			},
		},
		{
			// A Walker reads the document's object once: a second Object() on the
			// same walk starts where the first stopped, in the middle of the document.
			name: "two reads of the document's object",
			misuse: func(t *testing.T) error {
				t.Helper()

				w := open(t, `{"x": 1}`).Walker()

				read := func(string) error {
					_, err := w.Raw()

					return err
				}

				if err := w.Object([]jsondoc.Key{{Name: "x"}}, read); err != nil {
					t.Fatalf("the first Object() returned unexpected error: %v", err)
				}

				return w.Object([]jsondoc.Key{{Name: "x"}}, read)
			},
		},
		{
			// Null() reads the value when it is null, so a callback that has read
			// its value cannot ask afterwards whether it was null.
			name: "Null on a value read already",
			misuse: func(t *testing.T) error {
				t.Helper()

				w := open(t, `{"x": "a"}`).Walker()

				return w.Object([]jsondoc.Key{{Name: "x"}}, func(string) error {
					if _, err := w.String(); err != nil {
						return err
					}

					_, err := w.Null()

					return err
				})
			},
		},
		{
			// A read has already found the value is not a string, so the walk is
			// over: a callback that swallows that error and returns nil hides it,
			// and the walk must not blame the document for what is left over.
			name: "object value callback that swallows a wrong type error",
			misuse: func(t *testing.T) error {
				t.Helper()

				w := open(t, `{"x": {"a": 1}}`).Walker()

				return w.Object([]jsondoc.Key{{Name: "x"}}, func(string) error {
					_, _ = w.String()

					return nil
				})
			},
			wantAbsent: []string{"unknown key"},
		},
		{
			name: "array element callback that swallows a wrong type error",
			misuse: func(t *testing.T) error {
				t.Helper()

				w := open(t, `{"s": [{"a": 1}, "b"]}`).Walker()

				return w.Object([]jsondoc.Key{{Name: "s"}}, func(string) error {
					return w.Array("an array", func(int) error {
						_, _ = w.String()

						return nil
					})
				})
			},
			wantAbsent: []string{"unknown key"},
		},
		{
			// Array passes on an element callback's own error and records the
			// walk as failed. The parent callback swallows that error from Array
			// and returns nil, so the walk is asked to go on after a failed read:
			// that is the format's mistake, and the message must neither pass the
			// element's error on nor blame the document for it.
			name: "object value callback that swallows the error Array passed on",
			misuse: func(t *testing.T) error {
				t.Helper()

				w := open(t, `{"s": ["x", "y"]}`).Walker()

				return w.Object([]jsondoc.Key{{Name: "s"}}, func(string) error {
					_ = w.Array("an array", func(i int) error {
						if _, err := w.String(); err != nil {
							return err
						}

						if i == 0 {
							return errElementRefused
						}

						return nil
					})

					return nil
				})
			},
			wantNotWrapped: []error{errElementRefused},
		},
		{
			// Reading again after a read has failed cannot work: the failure says
			// the walk has given up, so the second read is the callback's mistake.
			name: "callback that swallows a wrong type error and reads again",
			misuse: func(t *testing.T) error {
				t.Helper()

				w := open(t, `{"x": {"a": 1}}`).Walker()

				return w.Object([]jsondoc.Key{{Name: "x"}}, func(string) error {
					_, _ = w.String()

					_, err := w.String()

					return err
				})
			},
			wantAbsent: []string{"unknown key"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.misuse(t)

			checkMisuseError(t, err)

			for _, unwanted := range tt.wantNotWrapped {
				if errors.Is(err, unwanted) {
					t.Errorf("error = %v, want it not to wrap the callback's own error %v", err, unwanted)
				}
			}

			msg := err.Error()
			for _, unwanted := range tt.wantAbsent {
				if strings.Contains(msg, unwanted) {
					t.Errorf("error = %q, want it without %s", msg, text.Quote(unwanted))
				}
			}
		})
	}
}

// TestLookupFirstOfRepeatedKeys pins that Lookup finds the first of two keys
// spelt the same: both formats read their schema version with Lookup before the
// walk refuses the repeated key, so the version they check is the one the object
// holds first.
func TestLookupFirstOfRepeatedKeys(t *testing.T) {
	t.Parallel()

	raw, folded, err := open(t, `{"v":1,"v":2}`).Lookup("v")
	if err != nil {
		t.Fatalf("Lookup() returned unexpected error: %v", err)
	}

	if folded != "" {
		t.Errorf("Lookup() folded = %s, want the empty spelling of an exact key", text.Quote(folded))
	}

	if diff := cmp.Diff("1", string(raw)); diff != "" {
		t.Errorf("Lookup() raw value mismatch (-want +got):\n%s", diff)
	}
}

// TestVersion pins the walk's re-check of a schema version: Version reads the
// value whole, as Raw does, so the walk checks the value the document holds
// rather than the one Document.Version read before it, and names that value by
// its Path.
func TestVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		doc  string
		// walk checks the version in the walk over doc and returns the error it
		// reports.
		walk func(t *testing.T, w *jsondoc.Walker) error
		// wantPath is the path the error names the value by, or "" when the
		// version is accepted.
		wantPath string
		// wantMentions lists substrings the message must contain.
		wantMentions []string
	}{
		{
			name: "key of the document object",
			doc:  `{"schemaVersion": 2}`,
			walk: func(t *testing.T, w *jsondoc.Walker) error {
				t.Helper()

				return w.Object([]jsondoc.Key{{Name: "schemaVersion"}}, func(string) error {
					return w.Version(1, errInvalid)
				})
			},
			wantPath:     "schemaVersion",
			wantMentions: []string{"is 2", "want the integer 1"},
		},
		{
			name: "element of an array",
			doc:  `{"values": [1, 2]}`,
			walk: func(t *testing.T, w *jsondoc.Walker) error {
				t.Helper()

				return w.Object([]jsondoc.Key{{Name: "values"}}, func(string) error {
					return w.Array("an array", func(i int) error {
						return w.Version(i+2, errInvalid)
					})
				})
			},
			wantPath:     "values[0]",
			wantMentions: []string{"is 1", "want the integer 2"},
		},
		{
			name: "null value",
			doc:  `{"schemaVersion": null}`,
			walk: func(t *testing.T, w *jsondoc.Walker) error {
				t.Helper()

				return w.Object([]jsondoc.Key{{Name: "schemaVersion"}}, func(string) error {
					return w.Version(1, errInvalid)
				})
			},
			wantPath:     "schemaVersion",
			wantMentions: []string{"is missing", "want the integer 1"},
		},
		{
			name: "accepted version",
			doc:  `{"schemaVersion": 1}`,
			walk: func(t *testing.T, w *jsondoc.Walker) error {
				t.Helper()

				return w.Object([]jsondoc.Key{{Name: "schemaVersion"}}, func(string) error {
					return w.Version(1, errInvalid)
				})
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.walk(t, open(t, tc.doc).Walker())

			if tc.wantPath == "" {
				if err != nil {
					t.Fatalf("Version() returned unexpected error: %v", err)
				}

				return
			}

			checkDocumentError(t, err)

			msg := err.Error()
			if !strings.Contains(msg, tc.wantPath) {
				t.Errorf("error = %q, want it to name the value %s by its path", msg, text.Quote(tc.wantPath))
			}

			for _, want := range tc.wantMentions {
				if !strings.Contains(msg, want) {
					t.Errorf("error = %q, want it to mention %s", msg, text.Quote(want))
				}
			}
		})
	}
}

// TestStringSurrogates pins that a string whose JSON text escapes a lone
// surrogate is refused, and that a correct surrogate pair is accepted and
// decoded: encoding/json would read a lone surrogate as U+FFFD, a value the
// document does not hold. A backslash escaped as "\\" is text, not an escape.
func TestStringSurrogates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
		// want is the decoded value when the string is accepted.
		want string
		// wantContains lists substrings the message must hold.
		wantContains []string
	}{
		{
			name:         "lone high surrogate",
			data:         `{"s": "\ud800"}`,
			wantContains: []string{"U+D800", "surrogate on its own"},
		},
		{
			name:         "lone low surrogate",
			data:         `{"s": "a\udc00b"}`,
			wantContains: []string{"U+DC00", "surrogate on its own"},
		},
		{
			name:         "high surrogate not followed by a low one",
			data:         `{"s": "\ud800x"}`,
			wantContains: []string{"U+D800", "surrogate on its own"},
		},
		{
			name: "correct surrogate pair",
			data: `{"s": "\ud83d\ude00"}`,
			want: "\U0001F600",
		},
		{
			name: "escaped backslash is text, not an escape",
			data: `{"s": "\\ud800"}`,
			want: `\ud800`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := open(t, tt.data).Walker()

			var got string

			err := w.Object([]jsondoc.Key{{Name: "s", Required: true}}, func(string) error {
				var err error

				got, err = w.String()

				return err
			})
			if len(tt.wantContains) == 0 {
				if err != nil {
					t.Fatalf("String() of %s returned unexpected error: %v", text.Quote(tt.data), err)
				}

				if diff := cmp.Diff(tt.want, got); diff != "" {
					t.Errorf("String() of %s mismatch (-want +got):\n%s", text.Quote(tt.data), diff)
				}

				return
			}

			checkDocumentError(t, err)

			msg := err.Error()
			for _, want := range tt.wantContains {
				if !strings.Contains(msg, want) {
					t.Errorf("String() error = %q, want it to contain %s", msg, text.Quote(want))
				}
			}
		})
	}
}

// TestValueTypes pins that each reader refuses a value of another JSON type,
// null included, naming the value by its path and the type it found, and that
// null is a value only where the format asks with Null first.
func TestValueTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
		// read reads the value of the document's "s" key.
		read func(w *jsondoc.Walker) error
		// wantContains lists substrings the message must hold.
		wantContains []string
		// wantQuoted lists values the message must show through text.Quote.
		wantQuoted []string
		// wantAbsent lists text the message must not hold: the keys and values a
		// document sent, which are data and are never echoed.
		wantAbsent []string
	}{
		{
			name:         "String of a number",
			data:         `{"s": 1}`,
			read:         readString,
			wantContains: []string{"is a number, want a string"},
			wantQuoted:   []string{"s"},
		},
		{
			name:         "String of an object",
			data:         `{"s": {}}`,
			read:         readString,
			wantContains: []string{"is an object, want a string"},
		},
		{
			name:         "String of null",
			data:         `{"s": null}`,
			read:         readString,
			wantContains: []string{"is null, want a string"},
		},
		{
			name:         "Array of an object",
			data:         `{"s": {}}`,
			read:         readArray,
			wantContains: []string{"is an object, want an array"},
		},
		{
			name:         "Array of null",
			data:         `{"s": null}`,
			read:         readArray,
			wantContains: []string{"is null, want an array"},
		},
		{
			name:         "Object of an array",
			data:         `{"s": []}`,
			read:         readObject,
			wantContains: []string{"is an array, want an object"},
		},
		{
			name:         "Object of a string",
			data:         `{"s": "x"}`,
			read:         readObject,
			wantContains: []string{"is a string, want an object"},
		},
		{
			name:         "Object of null",
			data:         `{"s": null}`,
			read:         readObject,
			wantContains: []string{"is null, want an object"},
		},
		{
			name:         "Bool of a string",
			data:         `{"s": "true"}`,
			read:         readBool,
			wantContains: []string{"is a string, want a boolean"},
			wantQuoted:   []string{"s"},
			wantAbsent:   []string{"true"},
		},
		{
			name:         "Bool of a number",
			data:         `{"s": 1}`,
			read:         readBool,
			wantContains: []string{"is a number, want a boolean"},
		},
		{
			name:         "Bool of null",
			data:         `{"s": null}`,
			read:         readBool,
			wantContains: []string{"is null, want a boolean"},
		},
		{
			name:         "Members of an array",
			data:         `{"s": []}`,
			read:         readMembers,
			wantContains: []string{"is an array, want an object of strings"},
		},
		{
			name:         "Members of a string",
			data:         `{"s": "x"}`,
			read:         readMembers,
			wantContains: []string{"is a string, want an object of strings"},
		},
		{
			name:         "Members of null",
			data:         `{"s": null}`,
			read:         readMembers,
			wantContains: []string{"is null, want an object of strings"},
		},
		{
			name:         "array element of the wrong type",
			data:         `{"s": [1]}`,
			read:         readArrayOfStrings,
			wantContains: []string{"is a number, want a string"},
			wantQuoted:   []string{"s[0]"},
		},
		{
			name:         "path shown through text.Quote",
			data:         `{"a\nb": 1}`,
			read:         readString,
			wantContains: []string{"is a number, want a string"},
			wantQuoted:   []string{"a\nb"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := open(t, tt.data).Walker()

			err := w.Object([]jsondoc.Key{{Name: "s"}, {Name: "a\nb"}}, func(string) error {
				return tt.read(w)
			})

			checkDocumentError(t, err)

			msg := err.Error()
			for _, want := range tt.wantContains {
				if !strings.Contains(msg, want) {
					t.Errorf("read error = %q, want it to contain %s", msg, text.Quote(want))
				}
			}

			for _, value := range tt.wantQuoted {
				if quoted := text.Quote(value); !strings.Contains(msg, quoted) {
					t.Errorf("read error = %q, want it to show %s through text.Quote", msg, quoted)
				}
			}

			for _, unwanted := range tt.wantAbsent {
				if strings.Contains(msg, unwanted) {
					t.Errorf("read error = %q, want it without %s, which is data, not a name", msg,
						text.Quote(unwanted))
				}
			}
		})
	}
}

// readString reads the walker's next value as a string.
func readString(w *jsondoc.Walker) error {
	_, err := w.String()

	return err
}

// readArray reads the walker's next value as an array of values read whole.
func readArray(w *jsondoc.Walker) error {
	return w.Array("an array", func(int) error {
		_, err := w.Raw()

		return err
	})
}

// readArrayOfStrings reads the walker's next value as an array of strings.
func readArrayOfStrings(w *jsondoc.Walker) error {
	return w.Array("an array of strings", func(int) error {
		_, err := w.String()

		return err
	})
}

// readObject reads the walker's next value as an object with no listed keys.
func readObject(w *jsondoc.Walker) error {
	return w.Object(nil, func(string) error { return nil })
}

// readBool reads the walker's next value as a boolean.
func readBool(w *jsondoc.Walker) error {
	_, err := w.Bool()

	return err
}

// readMembers reads the walker's next value as an object of strings.
func readMembers(w *jsondoc.Walker) error {
	return w.Members("an object of strings", func(int, string) error {
		_, err := w.String()

		return err
	})
}

// TestBool pins reading a boolean: true and false are read as they are, and a
// value of any other type, null included, is refused as a value of the wrong
// type.
func TestBool(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
		want bool
	}{
		{name: "true", data: `{"b": true}`, want: true},
		{name: "false", data: `{"b": false}`, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := open(t, tt.data).Walker()

			var got bool

			err := w.Object([]jsondoc.Key{{Name: "b"}}, func(string) error {
				var err error
				got, err = w.Bool()

				return err
			})
			if err != nil {
				t.Fatalf("Object() returned unexpected error: %v", err)
			}

			if got != tt.want {
				t.Errorf("Bool() = %t, want %t", got, tt.want)
			}
		})
	}
}

// readMembersOfRegions drives the walk over a document whose one key, regions,
// has a value read with Members, and returns the members read, in document
// order, and the error the walk stopped with. The keys are data, so a failure
// of the walk names them by index, never by the key itself.
func readMembersOfRegions(t *testing.T, data string) (keys, values []string, err error) {
	t.Helper()

	w := open(t, data).Walker()

	err = w.Object([]jsondoc.Key{{Name: "regions"}}, func(string) error {
		return w.Members("an object of strings", func(_ int, key string) error {
			value, err := w.String()
			if err != nil {
				return err
			}

			keys = append(keys, key)
			values = append(values, value)

			return nil
		})
	})

	return keys, values, err
}

// TestMembers pins reading an object whose keys are data: Members hands the
// format each member's index and its decoded key in document order, names a
// member's value by its index in brackets, places a repeated key by line and
// column only, and, because a key here is data rather than a name the format
// lists, never echoes a key or a value in an error.
func TestMembers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// regions is the JSON of the document's regions member.
		regions string
		// wantKeys and wantValues are the members read, in document order.
		wantKeys   []string
		wantValues []string
		// wantContains lists substrings an error must hold and wantQuoted ones
		// it must show through text.Quote.
		wantContains []string
		wantQuoted   []string
		// wantAbsent lists text the error must not hold: the keys and values
		// the document sent, which are data.
		wantAbsent []string
	}{
		{
			name:       "members in document order",
			regions:    `{"fr-par": "r1", "gra": "r2"}`,
			wantKeys:   []string{"fr-par", "gra"},
			wantValues: []string{"r1", "r2"},
		},
		{
			name:    "an empty object",
			regions: `{}`,
		},
		{
			name:       "a key with its JSON escapes decoded",
			regions:    `{"a\nb": "x"}`,
			wantKeys:   []string{"a\nb"},
			wantValues: []string{"x"},
		},
		{
			name:         "a member value of the wrong type",
			regions:      `{"api-key-1234": 1}`,
			wantContains: []string{"is a number, want a string"},
			wantQuoted:   []string{"regions[0]"},
			wantAbsent:   []string{"api-key-1234"},
		},
		{
			name:         "a member value of the wrong type, past a member that read",
			regions:      `{"a": "x", "b": [1]}`,
			wantContains: []string{"is an array, want a string"},
			wantQuoted:   []string{"regions[1]"},
			wantAbsent:   []string{"\"a\"", "\"b\""},
		},
		{
			name:         "a repeated member key",
			regions:      `{"leaky-key": "leaky-value", "leaky-key": "other-value"}`,
			wantContains: []string{"repeated key", "line 1, column 42"},
			wantQuoted:   []string{"regions"},
			wantAbsent:   []string{"leaky-key", "leaky-value", "other-value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			keys, values, err := readMembersOfRegions(t, `{"regions": `+tt.regions+`}`)

			if len(tt.wantContains) > 0 || len(tt.wantQuoted) > 0 || len(tt.wantAbsent) > 0 {
				checkDocumentError(t, err)

				msg := err.Error()

				for _, want := range tt.wantContains {
					if !strings.Contains(msg, want) {
						t.Errorf("read error = %q, want it to contain %s", msg, text.Quote(want))
					}
				}

				for _, value := range tt.wantQuoted {
					if quoted := text.Quote(value); !strings.Contains(msg, quoted) {
						t.Errorf("read error = %q, want it to show %s through text.Quote", msg, quoted)
					}
				}

				for _, unwanted := range tt.wantAbsent {
					if strings.Contains(msg, unwanted) {
						t.Errorf("read error = %q, want it without %s, which is data, not a name", msg,
							text.Quote(unwanted))
					}
				}

				return
			}

			if err != nil {
				t.Fatalf("the walk returned unexpected error: %v", err)
			}

			if diff := cmp.Diff(tt.wantKeys, keys); diff != "" {
				t.Errorf("the member keys mismatch (-want +got):\n%s", diff)
			}

			if diff := cmp.Diff(tt.wantValues, values); diff != "" {
				t.Errorf("the member values mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestNull pins the null policy the walker leaves to the format: Null reports
// true for a null value, so a format can treat it as absent, and false for any
// other value without reading it, so the value is still there to be read.
func TestNull(t *testing.T) {
	t.Parallel()

	doc := open(t, `{"absent": null, "present": "x"}`)
	w := doc.Walker()

	got := make(map[string]string)

	err := w.Object([]jsondoc.Key{{Name: "absent"}, {Name: "present"}}, func(key string) error {
		isNull, err := w.Null()
		if err != nil {
			return err
		}

		switch key {
		case "absent":
			if !isNull {
				t.Errorf("Null() for a null value = false, want true")
			}

			got[key] = "null"
		case "present":
			if isNull {
				t.Errorf("Null() for a string value = true, want false")
			}

			s, err := w.String()
			if err != nil {
				return err
			}

			got[key] = s
		}

		return nil
	})
	if err != nil {
		t.Fatalf("Object() returned unexpected error: %v", err)
	}

	want := map[string]string{"absent": "null", "present": "x"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("the values the walk read mismatch (-want +got):\n%s", diff)
	}
}

// TestPath pins the path the walker keeps for the value it reads next: the keys
// it is in joined by dots and, as the domain writes it, an array index in
// brackets.
func TestPath(t *testing.T) {
	t.Parallel()

	w := open(t, `{"items": [{"name": "a"}, {"name": "b"}]}`).Walker()

	if got := w.Path(); got != "" {
		t.Errorf("Path() before reading = %s, want the empty path of the document's object", text.Quote(got))
	}

	var got []string

	err := w.Object([]jsondoc.Key{{Name: "items", Required: true}}, func(string) error {
		got = append(got, w.Path())

		return w.Array("an array", func(int) error {
			got = append(got, w.Path())

			return w.Object([]jsondoc.Key{{Name: "name", Required: true}}, func(string) error {
				got = append(got, w.Path())

				_, err := w.String()

				return err
			})
		})
	})
	if err != nil {
		t.Fatalf("reading the document returned unexpected error: %v", err)
	}

	want := []string{"items", "items[0]", "items[0].name", "items[1]", "items[1].name"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("the paths the walker reported mismatch (-want +got):\n%s", diff)
	}
}

// TestPosition pins where the walker places the value it reads next: line and
// column both counted from 1, the column in characters, so the multi-byte key
// äöü counts three times, not its six bytes.
func TestPosition(t *testing.T) {
	t.Parallel()

	// Lines, so a case can tell the line and the column apart.
	const data = "{\n \"items\": [\n  {\"name\": \"a\"},\n  {\"name\": \"b\"}\n ],\n \"äöü\": 1\n}"

	w := open(t, data).Walker()

	if got := w.Position(); got != "line 1, column 1" {
		t.Errorf("Position() before reading = %q, want %q", got, "line 1, column 1")
	}

	got := make(map[string]string)

	err := w.Object(
		[]jsondoc.Key{{Name: "items", Required: true}, {Name: "äöü", Required: true}},
		func(key string) error {
			got[key] = w.Position()

			if key != "items" {
				_, err := w.Raw()

				return err
			}

			return w.Array("an array", func(i int) error {
				return w.Object([]jsondoc.Key{{Name: "name", Required: true}}, func(string) error {
					got["name at "+strconv.Itoa(i)] = w.Position()

					_, err := w.String()

					return err
				})
			})
		})
	if err != nil {
		t.Fatalf("reading the document returned unexpected error: %v", err)
	}

	want := map[string]string{
		"items":     "line 2, column 11",
		"name at 0": "line 3, column 12",
		"name at 1": "line 4, column 12",
		"äöü":       "line 6, column 9",
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("the positions the walker reported mismatch (-want +got):\n%s", diff)
	}
}

// TestCallbackErrorPassesThrough pins that an error a callback returns is passed
// on as it is, so a format reports its own sentinels through the walk without
// the decoder wrapping or replacing them.
func TestCallbackErrorPassesThrough(t *testing.T) {
	t.Parallel()

	errStop := errors.New("the format's own rule")

	w := open(t, `{"a": 1, "b": 2}`).Walker()

	err := w.Object([]jsondoc.Key{{Name: "a"}, {Name: "b"}}, func(key string) error {
		if key == "b" {
			return errStop
		}

		_, err := w.Raw()

		return err
	})
	if !errors.Is(err, errStop) {
		t.Fatalf("Object() error = %v, want the callback's own error %v", err, errStop)
	}

	// The error is passed on as it is, not wrapped or reworded: a wrapper would
	// add its own text around the callback's message.
	if err.Error() != errStop.Error() {
		t.Errorf("Object() error = %q, want the callback's message unchanged, %q", err, errStop.Error())
	}
}

// TestDescribe pins how a raw JSON value is named for an error message: an
// integer as written, any other number as written through text.Quote so a
// normalised form never replaces what the document holds, a string by its
// decoded value through text.Quote, and anything else by its type.
func TestDescribe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "no value", raw: "", want: "no value"},
		{name: "integer", raw: "2", want: "2"},
		{name: "negative integer", raw: "-1", want: "-1"},
		{name: "negative zero as written", raw: "-0", want: "the number " + text.Quote("-0")},
		{name: "decimal as written", raw: "1.0", want: "the number " + text.Quote("1.0")},
		{name: "exponent as written", raw: "1e0", want: "the number " + text.Quote("1e0")},
		{name: "string by its value", raw: `"a\nb"`, want: "the string " + text.Quote("a\nb")},
		{name: "object", raw: `{}`, want: "an object"},
		{name: "array", raw: `[]`, want: "an array"},
		{name: "true", raw: "true", want: "a boolean"},
		{name: "false", raw: "false", want: "a boolean"},
		{name: "null", raw: "null", want: "null"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tt.want, jsondoc.Describe([]byte(tt.raw))); diff != "" {
				t.Errorf("Describe(%s) mismatch (-want +got):\n%s", text.Quote(tt.raw), diff)
			}
		})
	}
}
