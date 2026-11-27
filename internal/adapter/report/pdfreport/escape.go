package pdfreport

import (
	"strconv"
	"strings"
)

// runeError is the rune a range loop over a string yields for a byte that does
// not start valid UTF-8, and also the valid character U+FFFD itself; the
// escaper tells the two apart by the bytes. The package does not import
// unicode/utf8 (depguard reporters-stay-offline).
const runeError = '�'

// runeErrorUTF8 is U+FFFD encoded as UTF-8.
const runeErrorUTF8 = "�"

// winAnsiHigh maps the 27 characters WinAnsiEncoding assigns to bytes 0x80 to
// 0x9F (ISO 32000-1, Annex D.2) to their bytes.
var winAnsiHigh = map[rune]byte{
	'€': 0x80, '‚': 0x82, 'ƒ': 0x83, '„': 0x84, '…': 0x85,
	'†': 0x86, '‡': 0x87, 'ˆ': 0x88, '‰': 0x89, 'Š': 0x8A,
	'‹': 0x8B, 'Œ': 0x8C, 'Ž': 0x8E, '‘': 0x91, '’': 0x92,
	'“': 0x93, '”': 0x94, '•': 0x95, '–': 0x96, '—': 0x97,
	'˜': 0x98, '™': 0x99, 'š': 0x9A, '›': 0x9B, 'œ': 0x9C,
	'ž': 0x9E, 'Ÿ': 0x9F,
}

// shownByte returns the WinAnsiEncoding byte of r and true when r is one of the
// characters the core fonts show and the escaper keeps: printable ASCII, the 27
// characters of 0x80 to 0x9F, and the Latin-1 Supplement from U+00A1, except
// U+00AD SOFT HYPHEN. U+00A0 NO-BREAK SPACE is not kept: it looks like a space.
func shownByte(r rune) (byte, bool) {
	switch {
	case r >= 0x20 && r <= 0x7E:
		return byte(r), true
	case r >= 0xA1 && r <= 0xFF && r != 0xAD:
		return byte(r), true
	default:
		b, ok := winAnsiHigh[r]

		return b, ok
	}
}

// escapeValue returns the text the PDF shows for a value from the inventory or
// a catalog: every character the core fonts show is kept, a backslash is
// doubled, every other rune is written as \u and four lowercase hex digits or
// \U and eight, and every byte of invalid UTF-8 as \x and two. It adds no
// quotes, uses no short escape and never truncates, so no rune is ever dropped
// or replaced.
func escapeValue(s string) string {
	var b strings.Builder

	b.Grow(len(s))

	for i, r := range s {
		switch {
		case r == runeError && !strings.HasPrefix(s[i:], runeErrorUTF8):
			b.WriteString(`\x`)
			b.WriteString(hexDigits(int64(s[i]), 2))
		case r == '\\':
			b.WriteString(`\\`)
		default:
			if _, ok := shownByte(r); ok {
				b.WriteRune(r)

				continue
			}

			b.WriteString(runeEscape(r))
		}
	}

	return b.String()
}

// runeEscape returns the visible escape of one rune: \u and four lowercase hex
// digits up to U+FFFF, \U and eight above.
func runeEscape(r rune) string {
	if r <= 0xFFFF {
		return `\u` + hexDigits(int64(r), 4)
	}

	return `\U` + hexDigits(int64(r), 8)
}

// hexDigits returns v in lowercase hex, padded with zeros to width digits.
func hexDigits(v int64, width int) string {
	digits := strconv.FormatInt(v, 16)
	if len(digits) < width {
		digits = strings.Repeat("0", width-len(digits)) + digits
	}

	return digits
}

// pdfString returns text, the escaped form of a value or the report's own
// words, as a PDF literal string: the outer parentheses, \( \) and \\ for the
// three characters a literal string treats as syntax, and a three-digit octal
// escape for every byte outside 0x20 to 0x7E, so the content stream stays
// printable ASCII. Each character is written as its WinAnsiEncoding byte.
//
// Text reaches pdfString already escaped, so it holds only characters the
// fonts show. Defensively, a rune with no WinAnsi byte is written as its
// visible escape, never dropped; a character below U+0100 outside the shown set
// (a control) is written as its byte, octal-escaped.
func pdfString(text string) string {
	var b strings.Builder

	b.Grow(len(text) + 2)
	b.WriteByte('(')

	for _, r := range text {
		c, ok := literalByte(r)
		if !ok {
			for _, e := range []byte(runeEscape(r)) {
				writeLiteralByte(&b, e)
			}

			continue
		}

		writeLiteralByte(&b, c)
	}

	b.WriteByte(')')

	return b.String()
}

// literalByte returns the byte a literal string writes for r: its WinAnsi byte
// for a shown character, its own byte for a control below 0x20, DEL and a
// character of U+00A0 to U+00FF, and false for anything else.
func literalByte(r rune) (byte, bool) {
	if c, ok := shownByte(r); ok {
		return c, true
	}

	switch {
	case r >= 0 && r < 0x80:
		return byte(r), true
	case r >= 0xA0 && r <= 0xFF:
		return byte(r), true
	}

	return 0, false
}

// writeLiteralByte writes one byte inside a literal string.
func writeLiteralByte(b *strings.Builder, c byte) {
	switch {
	case c == '(' || c == ')' || c == '\\':
		b.WriteByte('\\')
		b.WriteByte(c)
	case c >= 0x20 && c <= 0x7E:
		b.WriteByte(c)
	default:
		b.WriteByte('\\')
		b.WriteByte('0' + c>>6)
		b.WriteByte('0' + (c>>3)&7)
		b.WriteByte('0' + c&7)
	}
}

// atoms splits escaped text into the units a wrap may not split: one escape
// form (\\, \xNN, \uXXXX, \UXXXXXXXX) or one character.
func atoms(text string) []string {
	var out []string

	for i := 0; i < len(text); {
		size := atomSize(text[i:])
		out = append(out, text[i:i+size])
		i += size
	}

	return out
}

// atomSize returns the byte length of the unit text starts with.
func atomSize(text string) int {
	if text[0] == '\\' && len(text) > 1 {
		size := 0

		switch text[1] {
		case '\\':
			size = 2
		case 'x':
			size = 4
		case 'u':
			size = 6
		case 'U':
			size = 10
		}

		if size > 0 && size <= len(text) {
			return size
		}
	}

	for i, r := range text {
		if i > 0 {
			return i
		}

		if r == runeError && !strings.HasPrefix(text, runeErrorUTF8) {
			return 1
		}
	}

	return len(text)
}
