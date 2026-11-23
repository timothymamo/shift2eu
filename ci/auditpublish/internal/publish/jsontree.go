package publish

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// The result is read by a strict JSON parser of the core's own rather than by
// encoding/json: encoding/json keeps the last of two keys silently, turns
// invalid UTF-8 into U+FFFD, and accepts a lone surrogate escape, so a second
// severity could hide behind the first and an invalid byte would vanish
// before the schema saw it. The parser builds a tree that keeps every member
// of an object in order and marks a string that was not valid UTF-8, and the
// schema then refuses what it must with the field's path.

// jsonKind is the kind of a JSON value.
type jsonKind int

const (
	jsonNull jsonKind = iota
	jsonBool
	jsonNumber
	jsonString
	jsonArray
	jsonObject
)

// maxJSONDepth bounds the nesting the parser follows. The schema nests three
// levels deep (the result, a list, a finding); anything deeper is refused
// before the recursion could grow.
const maxJSONDepth = 8

// jsonValue is one parsed JSON value.
type jsonValue struct {
	kind jsonKind
	// text is a string's decoded text or a number's literal.
	text string
	// invalid marks a string that held invalid UTF-8 or a lone surrogate.
	invalid bool
	boolean bool
	items   []jsonValue
	members []jsonMember
}

// jsonMember is one member of an object, in the order the document gives it.
type jsonMember struct {
	key   string
	value jsonValue
}

// errJSONSyntax is a document that is no JSON. Its text names no byte of the
// document.
var errJSONSyntax = errors.New("not one JSON value")

// parseJSON parses data as exactly one JSON value, with only whitespace around
// it.
func parseJSON(data []byte) (jsonValue, error) {
	p := jsonParser{data: data}

	p.skipSpace()

	v, err := p.value(0)
	if err != nil {
		return jsonValue{}, err
	}

	p.skipSpace()

	if p.pos != len(p.data) {
		return jsonValue{}, errJSONSyntax
	}

	return v, nil
}

// jsonParser is the parser's state: the document and the offset it reads.
type jsonParser struct {
	data []byte
	pos  int
}

func (p *jsonParser) skipSpace() {
	for p.pos < len(p.data) {
		switch p.data[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *jsonParser) value(depth int) (jsonValue, error) {
	if depth > maxJSONDepth || p.pos >= len(p.data) {
		return jsonValue{}, errJSONSyntax
	}

	switch c := p.data[p.pos]; {
	case c == '{':
		return p.object(depth)
	case c == '[':
		return p.array(depth)
	case c == '"':
		text, invalid, err := p.str()

		return jsonValue{kind: jsonString, text: text, invalid: invalid}, err
	case c == 't':
		return jsonValue{kind: jsonBool, boolean: true}, p.literal("true")
	case c == 'f':
		return jsonValue{kind: jsonBool}, p.literal("false")
	case c == 'n':
		return jsonValue{kind: jsonNull}, p.literal("null")
	case c == '-' || c >= '0' && c <= '9':
		return p.number()
	default:
		return jsonValue{}, errJSONSyntax
	}
}

func (p *jsonParser) literal(word string) error {
	if !strings.HasPrefix(string(p.data[p.pos:min(len(p.data), p.pos+len(word))]), word) {
		return errJSONSyntax
	}

	p.pos += len(word)

	return nil
}

func (p *jsonParser) object(depth int) (jsonValue, error) {
	v := jsonValue{kind: jsonObject}

	p.pos++
	p.skipSpace()

	if p.pos < len(p.data) && p.data[p.pos] == '}' {
		p.pos++

		return v, nil
	}

	for {
		p.skipSpace()

		if p.pos >= len(p.data) || p.data[p.pos] != '"' {
			return jsonValue{}, errJSONSyntax
		}

		key, invalid, err := p.str()
		if err != nil || invalid {
			return jsonValue{}, errJSONSyntax
		}

		p.skipSpace()

		if p.pos >= len(p.data) || p.data[p.pos] != ':' {
			return jsonValue{}, errJSONSyntax
		}

		p.pos++
		p.skipSpace()

		member, err := p.value(depth + 1)
		if err != nil {
			return jsonValue{}, err
		}

		v.members = append(v.members, jsonMember{key: key, value: member})

		p.skipSpace()

		if p.pos >= len(p.data) {
			return jsonValue{}, errJSONSyntax
		}

		switch p.data[p.pos] {
		case ',':
			p.pos++
		case '}':
			p.pos++

			return v, nil
		default:
			return jsonValue{}, errJSONSyntax
		}
	}
}

func (p *jsonParser) array(depth int) (jsonValue, error) {
	v := jsonValue{kind: jsonArray}

	p.pos++
	p.skipSpace()

	if p.pos < len(p.data) && p.data[p.pos] == ']' {
		p.pos++

		return v, nil
	}

	for {
		p.skipSpace()

		item, err := p.value(depth + 1)
		if err != nil {
			return jsonValue{}, err
		}

		v.items = append(v.items, item)

		p.skipSpace()

		if p.pos >= len(p.data) {
			return jsonValue{}, errJSONSyntax
		}

		switch p.data[p.pos] {
		case ',':
			p.pos++
		case ']':
			p.pos++

			return v, nil
		default:
			return jsonValue{}, errJSONSyntax
		}
	}
}

// number reads a number's literal as JSON's grammar states it; the schema
// decides whether it is an integer in range.
func (p *jsonParser) number() (jsonValue, error) {
	start := p.pos

	if p.data[p.pos] == '-' {
		p.pos++
	}

	switch {
	case p.pos < len(p.data) && p.data[p.pos] == '0':
		p.pos++
	case p.pos < len(p.data) && p.data[p.pos] >= '1' && p.data[p.pos] <= '9':
		p.digits()
	default:
		return jsonValue{}, errJSONSyntax
	}

	if p.pos < len(p.data) && p.data[p.pos] == '.' {
		p.pos++

		if p.digits() == 0 {
			return jsonValue{}, errJSONSyntax
		}
	}

	if p.pos < len(p.data) && (p.data[p.pos] == 'e' || p.data[p.pos] == 'E') {
		p.pos++

		if p.pos < len(p.data) && (p.data[p.pos] == '+' || p.data[p.pos] == '-') {
			p.pos++
		}

		if p.digits() == 0 {
			return jsonValue{}, errJSONSyntax
		}
	}

	return jsonValue{kind: jsonNumber, text: string(p.data[start:p.pos])}, nil
}

func (p *jsonParser) digits() int {
	start := p.pos

	for p.pos < len(p.data) && p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
		p.pos++
	}

	return p.pos - start
}

// str reads a string. A raw control byte is a syntax error, as JSON states;
// invalid UTF-8 and a lone surrogate escape are kept as a mark, so the schema
// can refuse them with the field's path.
func (p *jsonParser) str() (string, bool, error) {
	p.pos++

	var (
		b       strings.Builder
		invalid bool
	)

	for {
		if p.pos >= len(p.data) {
			return "", false, errJSONSyntax
		}

		c := p.data[p.pos]

		switch {
		case c == '"':
			p.pos++

			return b.String(), invalid, nil
		case c == '\\':
			r, bad, err := p.escape()
			if err != nil {
				return "", false, err
			}

			invalid = invalid || bad

			b.WriteRune(r)
		case c < 0x20:
			return "", false, errJSONSyntax
		case c < utf8.RuneSelf:
			b.WriteByte(c)
			p.pos++
		default:
			r, size := utf8.DecodeRune(p.data[p.pos:])
			if r == utf8.RuneError && size <= 1 {
				invalid = true
				p.pos++

				continue
			}

			b.WriteRune(r)
			p.pos += size
		}
	}
}

// escape reads one escape sequence at p.pos and returns its rune, and whether
// it was a lone surrogate.
func (p *jsonParser) escape() (rune, bool, error) {
	if p.pos+1 >= len(p.data) {
		return 0, false, errJSONSyntax
	}

	c := p.data[p.pos+1]
	p.pos += 2

	switch c {
	case '"':
		return '"', false, nil
	case '\\':
		return '\\', false, nil
	case '/':
		return '/', false, nil
	case 'b':
		return '\b', false, nil
	case 'f':
		return '\f', false, nil
	case 'n':
		return '\n', false, nil
	case 'r':
		return '\r', false, nil
	case 't':
		return '\t', false, nil
	case 'u':
	default:
		return 0, false, errJSONSyntax
	}

	r, err := p.hex4()
	if err != nil {
		return 0, false, err
	}

	switch {
	case r >= 0xd800 && r < 0xdc00:
		// A high surrogate needs a low one right after it.
		if p.pos+1 < len(p.data) && p.data[p.pos] == '\\' && p.data[p.pos+1] == 'u' {
			save := p.pos
			p.pos += 2

			low, err := p.hex4()
			if err != nil {
				return 0, false, err
			}

			if low >= 0xdc00 && low < 0xe000 {
				return 0x10000 + ((r - 0xd800) << 10) + (low - 0xdc00), false, nil
			}

			p.pos = save
		}

		return utf8.RuneError, true, nil
	case r >= 0xdc00 && r < 0xe000:
		return utf8.RuneError, true, nil
	default:
		return r, false, nil
	}
}

func (p *jsonParser) hex4() (rune, error) {
	if p.pos+4 > len(p.data) {
		return 0, errJSONSyntax
	}

	var r rune

	for _, c := range p.data[p.pos : p.pos+4] {
		r <<= 4

		switch {
		case c >= '0' && c <= '9':
			r |= rune(c - '0')
		case c >= 'a' && c <= 'f':
			r |= rune(c-'a') + 10
		case c >= 'A' && c <= 'F':
			r |= rune(c-'A') + 10
		default:
			return 0, errJSONSyntax
		}
	}

	p.pos += 4

	return r, nil
}
