package jsondoc

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/timothymamo/shift2eu/internal/domain/text"
)

// MaxKeys is the most keys one kind of object may list: Walker.Object records
// the keys an object has in one bit each.
const MaxKeys = 64

// Key is one key an object of a format may have.
type Key struct {
	// Name is the key, spelt as the format defines it. A key of the document
	// matches it only character for character, once JSON escapes are decoded.
	Name string
	// Required is whether Walker.Object refuses an object without the key.
	Required bool
}

// Walker reads a Document token by token, holding one token at a time however
// large the document or its values, and keeps the path of the value it reads
// next, which its errors name. A format drives it from the document's object
// down: Walker.Object calls back for each key, and the callback reads the key's
// value with one of the methods below, which may call back in turn. Get a
// Walker from Document.Walker; a Walker is not safe for concurrent use.
//
// An error from any read method stops the walk, and the callback that gets it
// returns it, so it reaches the format as it is. After a read has returned an
// error, a further read, and a callback that returns no error, are refused as
// ErrMisuse: the walker may be inside the value that failed, and what it would
// read next is no fault of the document.
type Walker struct {
	data    []byte
	dec     *json.Decoder
	invalid error
	// path holds a segment for each object key and array element the walker is
	// in, outermost first.
	path []segment
	// read is set once the document's object, the value of the empty path, has
	// started to be read: a walker reads it once.
	read bool
	// failed is set once a read method has returned an error, which stops the
	// walk: the walker may be inside the value that failed, so a later read
	// would blame the document for what a callback left over.
	failed bool
}

// segment is one step of a path: an object key, or an array index when index
// is not negative. read is set once the callback for the key or element has
// started to read its value, so a callback that reads it a second time, or
// returns without reading it, is caught.
type segment struct {
	key   string
	index int
	read  bool
}

// Object reads the next value, which must be an object whose keys are among
// keys (at most MaxKeys). For each key of the object, in document order, it
// calls value with the key's Name, with the walker at the key's value and Path
// naming it. value must read that value whole, with one call of Object,
// Members, Array, String, Bool, Raw or Version, or with Null and, when Null returns false, one of
// those, or return an error.
//
// Object refuses, with an error that wraps the format's sentinel: a value that
// is not an object, null included; a key that escapes a lone surrogate; a key
// that is not character for character the Name of one of keys, with a hint
// when it equals one ignoring case; a key the object already has; and, once the
// object closes, a Required key it lacks. An error value returns is passed on
// as it is, and stops the walk. keys longer than MaxKeys or holding a Name
// twice, a value callback that reads its value a second time or returns no
// error without reading it, and a value read a second time are refused as
// ErrMisuse, as is any read, or a callback that returns no error, after a read
// has failed (see Walker).
func (w *Walker) Object(keys []Key, value func(key string) error) (err error) {
	defer w.stop(&err)

	if err := checkKeys(keys); err != nil {
		return err
	}

	tok, start, err := w.value()
	if err != nil {
		return err
	}

	if tok != json.Delim('{') {
		return w.wrongType(start, "an object")
	}

	var seen uint64

	for w.dec.More() {
		i, err := w.key(keys, seen)
		if err != nil {
			return err
		}

		seen |= 1 << i

		w.enter(segment{key: keys[i].Name, index: -1})

		if err := w.leave(value(keys[i].Name)); err != nil {
			return err
		}
	}

	if _, _, err := w.next(); err != nil {
		return err
	}

	for i, key := range keys {
		if !key.Required || seen&(1<<i) != 0 {
			continue
		}

		if len(w.path) == 0 {
			return fmt.Errorf("%w: required key %s is missing from the document", w.invalid, text.Quote(key.Name))
		}

		return fmt.Errorf("%w: required key %s is missing from %s, which starts at %s",
			w.invalid, text.Quote(key.Name), text.Quote(w.Path()), position(w.data, start))
	}

	return nil
}

// checkKeys refuses keys, the keys a format lists for one kind of object, with
// an error that wraps ErrMisuse, if they are more than MaxKeys or hold a Name
// twice, which leaves no way to tell which of the two an object's key is.
func checkKeys(keys []Key) error {
	if len(keys) > MaxKeys {
		return fmt.Errorf("%w: an object may list at most %d keys, not %d", ErrMisuse, MaxKeys, len(keys))
	}

	for i := range keys {
		for j := range i {
			if keys[j].Name == keys[i].Name {
				return fmt.Errorf("%w: the keys listed for an object hold %s twice", ErrMisuse, text.Quote(keys[i].Name))
			}
		}
	}

	return nil
}

// Array reads the next value, which must be an array, and calls element with the
// index of each of its elements, in order, with the walker at the element and
// Path naming it. element must read the element whole, as Object's value must
// read a key's value, or return an error. A value that is not an array, null
// included, is refused as not want, which names, with its article, the type
// the format expects ("an array", "an array of strings"). An error element
// returns is passed on as it is and stops the walk, so a format refuses an
// array at its first element past a cap by returning an error before it reads
// that element. An element callback that reads its element a second time or
// returns no error without reading it, and a value read a second time, are
// refused as ErrMisuse, as is any read, or a callback that returns no error,
// after a read has failed (see Walker).
func (w *Walker) Array(want string, element func(i int) error) (err error) {
	defer w.stop(&err)

	tok, at, err := w.value()
	if err != nil {
		return err
	}

	if tok != json.Delim('[') {
		return w.wrongType(at, want)
	}

	for i := 0; w.dec.More(); i++ {
		w.enter(segment{index: i})

		if err := w.leave(element(i)); err != nil {
			return err
		}
	}

	_, _, err = w.next()

	return err
}

// String reads the next value, which must be a string, and returns it with its
// JSON escapes decoded. It refuses a value that is not a string, null included,
// and a string that escapes a lone surrogate.
func (w *Walker) String() (_ string, err error) {
	defer w.stop(&err)

	tok, at, err := w.value()
	if err != nil {
		return "", err
	}

	s, ok := tok.(string)
	if !ok {
		return "", w.wrongType(at, "a string")
	}

	if err := w.checkString(s, at, func() string { return text.Quote(w.Path()) }); err != nil {
		return "", err
	}

	return s, nil
}

// Bool reads the next value, which must be true or false, and returns it. It
// refuses a value that is not a boolean, null included, as String refuses a
// value that is not a string.
func (w *Walker) Bool() (_ bool, err error) {
	defer w.stop(&err)

	tok, at, err := w.value()
	if err != nil {
		return false, err
	}

	b, ok := tok.(bool)
	if !ok {
		return false, w.wrongType(at, "a boolean")
	}

	return b, nil
}

// Members reads the next value, which must be an object whose keys are data
// rather than names a format lists, such as a map from a target key to a
// region. For each member, in document order, it calls value with the
// member's index in the object and its key, JSON escapes decoded, with the
// walker at the member's value. value must read that value whole, as Object's
// value must, or return an error; a format caps the members by returning an
// error at the first index past its cap, before it reads that value.
//
// Because the keys are data, no error of this package quotes one: Path names a
// member's value by its index in brackets, as an array element is named
// (regions[0]), and a repeated key is placed by line and column only. Members
// refuses, with an error that wraps the format's sentinel: a value that is not
// an object, null included, as not want, which names the type with its
// article ("an object of strings"); a key that escapes a lone surrogate; and a
// key the object already has, character for character. Misuse is refused as
// Object refuses it.
func (w *Walker) Members(want string, value func(i int, key string) error) (err error) {
	defer w.stop(&err)

	tok, start, err := w.value()
	if err != nil {
		return err
	}

	if tok != json.Delim('{') {
		return w.wrongType(start, want)
	}

	seen := make(map[string]struct{})

	for i := 0; w.dec.More(); i++ {
		key, err := w.member(seen)
		if err != nil {
			return err
		}

		w.enter(segment{index: i})

		if err := w.leave(value(i, key)); err != nil {
			return err
		}
	}

	_, _, err = w.next()

	return err
}

// member reads the next key of an object Members reads, of which seen holds
// the keys the object has had so far, and adds it to seen. It refuses a key
// that escapes a lone surrogate or that seen holds already, naming neither key.
func (w *Walker) member(seen map[string]struct{}) (string, error) {
	tok, at, err := w.next()
	if err != nil {
		return "", err
	}

	key, ok := tok.(string)
	if !ok {
		// Only a value a callback left unread puts anything but a string here.
		return "", w.lost(at)
	}

	what := func() string { return "a key of " + text.Quote(w.Path()) }

	if err := w.checkString(key, at, what); err != nil {
		return "", err
	}

	if _, ok := seen[key]; ok {
		return "", fmt.Errorf("%w: repeated key in %s at %s", w.invalid, text.Quote(w.Path()), position(w.data, at))
	}

	seen[key] = struct{}{}

	return key, nil
}

// Null reads the next value if it is null, and reports whether it was. When it
// was not, Null reads nothing, and the value is still to be read. A format reads
// a field that may be null, as an absent value, by calling Null first; a field
// whose value is read without Null refuses null as a value of the wrong type.
// Null on a value read already is refused as ErrMisuse.
func (w *Walker) Null() (_ bool, err error) {
	defer w.stop(&err)

	if err := w.readable(); err != nil {
		return false, err
	}

	// The document is well formed, so a value that starts with n is null.
	if at := skipSeparators(w.data, int(w.dec.InputOffset())); at >= len(w.data) || w.data[at] != 'n' {
		return false, nil
	}

	if _, _, err := w.value(); err != nil {
		return false, err
	}

	return true, nil
}

// Raw reads the next value whole, objects and arrays in it included, and
// returns its JSON text as the document spells it. It checks nothing in the
// value: a format reads a value with Raw to check it itself, as it checks its
// schema version, and Describe names it in an error.
func (w *Walker) Raw() (_ []byte, err error) {
	defer w.stop(&err)

	if err := w.begin(); err != nil {
		return nil, err
	}

	start, err := w.skip()
	if err != nil {
		return nil, err
	}

	return w.data[start:w.dec.InputOffset()], nil
}

// Version reads the next value whole, as Raw does, and checks it against want
// as Document.Version checks a schema version, naming it by its Path, with
// errors that wrap sentinel. A format calls it in the walk for the value of its
// schema version key, which it has read first with Document.Version, so the
// walk checks the value it reads instead of relying on that earlier read. A nil
// sentinel is refused as ErrMisuse.
func (w *Walker) Version(want int, sentinel error) (err error) {
	defer w.stop(&err)

	if sentinel == nil {
		return errNilSentinel("Version")
	}

	raw, err := w.Raw()
	if err != nil {
		return err
	}

	return checkVersion(w.Path(), raw, "", want, sentinel)
}

// Path returns the path of the value the walker reads next, as the domain
// writes it: the keys it is in joined by dots, and the index of an array
// element in brackets (resources[1].capabilities[0]). It is "" for the
// document's object. In Object's value it names the key's value, and in
// Array's element the element.
func (w *Walker) Path() string {
	var b strings.Builder

	for i, s := range w.path {
		if s.index >= 0 {
			b.WriteString("[" + strconv.Itoa(s.index) + "]")

			continue
		}

		if i > 0 {
			b.WriteByte('.')
		}

		b.WriteString(s.key)
	}

	return b.String()
}

// Position returns where the value the walker reads next starts, as "line L,
// column C", both counted from 1 and the column in characters, as every error
// of this package places a problem. On a fresh Walker it is where the
// document's object starts.
func (w *Walker) Position() string {
	return position(w.data, skipSeparators(w.data, int(w.dec.InputOffset())))
}

// enter puts s, the key or element whose value a callback is about to read, on
// the path, with its value still to be read.
func (w *Walker) enter(s segment) {
	w.path = append(w.path, s)
}

// leave takes the segment enter put on the path off it, once the callback has
// returned err. It passes err on, and refuses a callback that returned no error
// after a read failed, or without reading its value.
func (w *Walker) leave(err error) error {
	if err == nil {
		switch {
		case w.failed:
			err = fmt.Errorf("%w: the callback for the value at %s went on after the walk failed; return the error",
				ErrMisuse, text.Quote(w.Path()))
		case !w.path[len(w.path)-1].read:
			err = fmt.Errorf("%w: the value at %s was not read", ErrMisuse, text.Quote(w.Path()))
		}
	}

	w.path = w.path[:len(w.path)-1]

	return err
}

// stop records that the walk has failed when *err, the error a read method
// returns, is not nil. A read method defers it.
func (w *Walker) stop(err *error) {
	if *err != nil {
		w.failed = true
	}
}

// value starts reading the next value (begin) and returns its first token and
// the index in the document of its first byte.
func (w *Walker) value() (json.Token, int, error) {
	if err := w.begin(); err != nil {
		return nil, 0, err
	}

	return w.next()
}

// begin records that the value the walker is at, the value of the innermost
// segment of the path or the document's object, is being read, once readable
// has allowed it.
func (w *Walker) begin() error {
	if err := w.readable(); err != nil {
		return err
	}

	if n := len(w.path); n > 0 {
		w.path[n-1].read = true
	} else {
		w.read = true
	}

	return nil
}

// readable refuses, as ErrMisuse, to read the value the walker is at when the
// walker has no document, the zero Document's, when a read has failed already,
// which stops the walk, or when it has started to read that value already: a
// callback reads its value once, and a walker the document's object once.
func (w *Walker) readable() error {
	if err := w.checkDocument(); err != nil {
		return err
	}

	if w.failed {
		what := "the document's object"
		if len(w.path) > 0 {
			what = "the value at " + text.Quote(w.Path())
		}

		return fmt.Errorf("%w: a read of %s went on after the walk failed; return the error", ErrMisuse, what)
	}

	if n := len(w.path); n > 0 && w.path[n-1].read {
		return fmt.Errorf("%w: the value at %s is read a second time", ErrMisuse, text.Quote(w.Path()))
	}

	if len(w.path) == 0 && w.read {
		return fmt.Errorf("%w: the document's object is read a second time", ErrMisuse)
	}

	return nil
}

// checkDocument refuses, as ErrMisuse, a walker of the zero Document, which has
// no document to read and no sentinel to wrap. Open never returns a Document
// without its sentinel.
func (w *Walker) checkDocument() error {
	if w.invalid == nil {
		return fmt.Errorf("%w: the zero Document holds no document; get one from Open", ErrMisuse)
	}

	return nil
}

// next returns the next token and the index in the document of its first byte.
func (w *Walker) next() (json.Token, int, error) {
	at := skipSeparators(w.data, int(w.dec.InputOffset()))

	tok, err := w.dec.Token()
	if err != nil {
		// Open has checked the syntax already, so the decoder fails here only
		// if the walker has lost its place.
		return nil, at, w.lost(at)
	}

	return tok, at, nil
}

// key reads the next key of an object whose keys are keys, of which seen
// records those the object has had so far, and returns its index in keys. It
// refuses a key that escapes a lone surrogate, that is not character for
// character the Name of one of keys, or that seen already records.
func (w *Walker) key(keys []Key, seen uint64) (int, error) {
	tok, at, err := w.next()
	if err != nil {
		return 0, err
	}

	key, ok := tok.(string)
	if !ok {
		// Only a value a callback left unread puts anything but a string here.
		return 0, w.lost(at)
	}

	what := func() string {
		if len(w.path) == 0 {
			return "a key"
		}

		return "a key of " + text.Quote(w.Path())
	}

	if err := w.checkString(key, at, what); err != nil {
		return 0, err
	}

	i := slices.IndexFunc(keys, func(k Key) bool { return k.Name == key })
	if i < 0 {
		hint := ""
		if j := slices.IndexFunc(keys, func(k Key) bool { return strings.EqualFold(k.Name, key) }); j >= 0 {
			hint = " (keys are exact: did you mean " + text.Quote(keys[j].Name) + "?)"
		}

		return 0, fmt.Errorf("%w: unknown key %s at %s%s", w.invalid, text.Quote(key), w.where(key, at), hint)
	}

	if seen&(1<<i) != 0 {
		return 0, fmt.Errorf("%w: repeated key %s at %s", w.invalid, text.Quote(key), w.where(key, at))
	}

	return i, nil
}

// skip steps over the next value, objects and arrays included, and returns the
// index in the document of its first byte.
func (w *Walker) skip() (int, error) {
	depth := 0

	tok, at, err := w.next()

	for ; err == nil; tok, _, err = w.next() {
		switch tok {
		case json.Delim('{'), json.Delim('['):
			depth++
		case json.Delim('}'), json.Delim(']'):
			depth--
		}

		if depth == 0 {
			return at, nil
		}
	}

	return at, err
}

// checkString refuses s, a string token that starts at index at of the
// document, if its JSON text escapes a lone surrogate: a high surrogate escape
// (D800 to DBFF) not followed at once by a low one (DC00 to DFFF), or a low one
// on its own. Such an escape names no character, and encoding/json decodes it to
// U+FFFD, so the value would differ from the document. what names the string in
// the error and is called only then. The JSON text is scanned only when s holds
// U+FFFD, which every lone surrogate decodes to; Open has refused invalid UTF-8
// already.
func (w *Walker) checkString(s string, at int, what func() string) error {
	if !strings.ContainsRune(s, utf8.RuneError) {
		return nil
	}

	raw := w.data[at:w.dec.InputOffset()]

	i, unit := loneSurrogate(raw)
	if i < 0 {
		return nil
	}

	return fmt.Errorf("%w: %s escapes U+%04X, a surrogate on its own, which is no character, at %s",
		w.invalid, what(), unit, position(w.data, at+i))
}

// wrongType reports that the value at Path, which starts at index at of the
// document, is not of the JSON type want. The walk stops there, so a value that
// is an object or an array is never stepped over.
func (w *Walker) wrongType(at int, want string) error {
	if len(w.path) == 0 {
		return fmt.Errorf("%w: the document is %s, want %s, at %s", w.invalid, kind(w.data[at]), want, position(w.data, at))
	}

	return fmt.Errorf("%w: %s is %s, want %s, at %s",
		w.invalid, text.Quote(w.Path()), kind(w.data[at]), want, position(w.data, at))
}

// where names the place of key, which starts at index at of the document, in
// the object at Path: the key's path and its line and column, or only its line
// and column for a key of the document's object, whose path is the key itself.
func (w *Walker) where(key string, at int) string {
	if len(w.path) == 0 {
		return position(w.data, at)
	}

	return text.Quote(w.Path()+"."+key) + ", " + position(w.data, at)
}

// lost reports, as ErrMisuse, that the walker cannot read the token at index at
// of the document: it has lost its place. Open has checked the document's
// syntax and every read is checked by readable, so only a format that misuses
// the walker in a way readable cannot see gets here.
func (w *Walker) lost(at int) error {
	return fmt.Errorf("%w: the walker has lost its place in the document at %s", ErrMisuse, position(w.data, at))
}

// escapeLen is the length of a JSON escape of one UTF-16 code unit: a
// backslash, u and four hexadecimal digits.
const escapeLen = 6

// loneSurrogate returns the index in raw, the JSON text of a well-formed string,
// of the first escape of a lone surrogate, and the surrogate, or -1 if there is
// none. It steps over every other escape, so an escaped backslash followed by u
// is never read as an escape of its own.
func loneSurrogate(raw []byte) (int, rune) {
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' {
			continue
		}

		r, ok := escapedUnit(raw, i)
		if !ok {
			// Any other escape is two bytes: step over the escaped byte.
			i++

			continue
		}

		if utf16.IsSurrogate(r) {
			// A pair decodes to one character; anything else is U+FFFD.
			if low, ok := escapedUnit(raw, i+escapeLen); !ok || utf16.DecodeRune(r, low) == unicode.ReplacementChar {
				return i, r
			}

			i += escapeLen
		}

		i += escapeLen - 1
	}

	return -1, 0
}

// escapedUnit returns the UTF-16 code unit escaped at index i of raw, and
// whether raw holds such an escape there: a backslash, u and four hexadecimal
// digits.
func escapedUnit(raw []byte, i int) (rune, bool) {
	if i+escapeLen > len(raw) || raw[i] != '\\' || raw[i+1] != 'u' {
		return 0, false
	}

	unit, err := strconv.ParseUint(string(raw[i+2:i+escapeLen]), 16, 16)
	if err != nil {
		return 0, false
	}

	return rune(unit), true
}
