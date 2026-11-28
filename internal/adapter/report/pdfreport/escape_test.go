package pdfreport_test

// The escaper's own table test: every character class of the PRD's Characters
// section, with the exact form the PDF shows, and the literal string form the
// content stream writes.
//
// Every expected value is an ASCII-only Go string literal built with \x and \u
// escapes, so no editor or tool can turn an expected escape such as \u0416 into
// the character it names.

import (
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/internal/adapter/report/pdfreport"
	"github.com/timothymamo/shift2eu/internal/domain/text"
)

// TestEscapeValue pins the escaper: it keeps every character the core fonts show
// through WinAnsiEncoding, writes every other rune as a visible escape in Go's
// strconv style with lowercase hex, adds no quotes, uses no short escape and
// never truncates.
func TestEscapeValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "printable ASCII stays as it is",
			in:   " !\"#$%&'()*+,-./0123456789:;<=>?@AZaz[\\]^_`{|}~",
			want: " !\"#$%&'()*+,-./0123456789:;<=>?@AZaz[\\\\]^_`{|}~",
		},
		{name: "a double quote stays, unquoted", in: "\"", want: "\""},
		{name: "a backslash is doubled", in: `a\b`, want: `a\\b`},
		{name: "two backslashes are four", in: `\\`, want: `\\\\`},
		{
			name: "a value that spells an escape is doubled, so it can never be taken for one",
			in:   `\u0416`,
			want: `\\u0416`,
		},
		{
			name: "every WinAnsi character of 0x80 to 0x9F is kept",
			in: "\u20ac\u201a\u0192\u201e\u2026\u2020\u2021\u02c6\u2030\u0160\u2039\u0152\u017d" +
				"\u2018\u2019\u201c\u201d\u2022\u2013\u2014\u02dc\u2122\u0161\u203a\u0153\u017e\u0178",
			want: "\u20ac\u201a\u0192\u201e\u2026\u2020\u2021\u02c6\u2030\u0160\u2039\u0152\u017d" +
				"\u2018\u2019\u201c\u201d\u2022\u2013\u2014\u02dc\u2122\u0161\u203a\u0153\u017e\u0178",
		},
		{
			name: "the Latin-1 Supplement is kept",
			in:   "\u00a1\u00a2\u00e9\u00d7\u00f7\u00ff",
			want: "\u00a1\u00a2\u00e9\u00d7\u00f7\u00ff",
		},
		{name: "no-break space is escaped, never shown as a blank", in: "\u00a0", want: `\u00a0`},
		{name: "soft hyphen is escaped, never shown", in: "\u00ad", want: `\u00ad`},
		{name: "no-break space inside a value is escaped too", in: "a\u00a0b", want: `a\u00a0b`},
		{name: "NUL is escaped", in: "\u0000", want: `\u0000`},
		{name: "a bell is escaped", in: "\u0007", want: `\u0007`},
		{name: "a tab is escaped with four digits, never as a short escape", in: "\u0009", want: `\u0009`},
		{name: "a line feed is escaped with four digits, never as a short escape", in: "\u000a", want: `\u000a`},
		{name: "a carriage return is escaped", in: "\u000d", want: `\u000d`},
		{name: "an escape character is escaped", in: "\u001b", want: `\u001b`},
		{name: "DEL is escaped", in: "\u007f", want: `\u007f`},
		{name: "a C1 control is escaped", in: "\u0085", want: `\u0085`},
		{name: "the last C1 control is escaped", in: "\u009f", want: `\u009f`},
		{name: "a line separator is escaped", in: "\u2028", want: `\u2028`},
		{name: "a paragraph separator is escaped", in: "\u2029", want: `\u2029`},
		{name: "a left-to-right mark is escaped", in: "\u200e", want: `\u200e`},
		{name: "a right-to-left override is escaped", in: "\u202e", want: `\u202e`},
		{name: "a zero-width joiner is escaped", in: "\u200d", want: `\u200d`},
		{name: "a Cyrillic letter is escaped, never kept and never dropped", in: "\u0416", want: `\u0416`},
		{name: "a CJK character is escaped", in: "\u4e2d", want: `\u4e2d`},
		{name: "the highest BMP code point is escaped", in: "\uffff", want: `\uffff`},
		{name: "a character above U+FFFF is escaped with eight digits", in: "\U0001f600", want: `\U0001f600`},
		{name: "the first character above U+FFFF is escaped with eight digits", in: "\U00010000", want: `\U00010000`},
		{name: "the highest code point is escaped with eight digits", in: "\U0010ffff", want: `\U0010ffff`},
		{name: "an invalid byte is escaped with two digits", in: "\xff", want: `\xff`},
		{name: "a truncated sequence is escaped byte by byte", in: "\xc3", want: `\xc3`},
		{name: "a continuation byte alone is escaped", in: "\x80", want: `\x80`},
		{
			name: "invalid bytes around a valid ASCII byte are escaped one by one",
			in:   "\xe2\x28\xa1",
			want: `\xe2(\xa1`,
		},
		{
			name: "kept and escaped characters mix",
			in:   "\u00e9 \u0416 \u20ac \U0001f600",
			want: "\u00e9 " + `\u0416` + " \u20ac " + `\U0001f600`,
		},
		{name: "the empty value stays empty", in: "", want: ""},
		{
			name: "a value of text.MaxLen bytes is escaped in full, never truncated",
			in:   strings.Repeat("a", text.MaxLen),
			want: strings.Repeat("a", text.MaxLen),
		},
		{
			name: "1024 two-byte runes are escaped in full, 6144 characters",
			in:   strings.Repeat("\u0416", text.MaxLen/2),
			want: strings.Repeat(`\u0416`, text.MaxLen/2),
		},
		{
			name: "2048 invalid bytes are escaped in full, 8192 characters",
			in:   strings.Repeat("\xff", text.MaxLen),
			want: strings.Repeat(`\xff`, text.MaxLen),
		},
		{
			name: "2048 backslashes are doubled in full, 4096 characters",
			in:   strings.Repeat(`\`, text.MaxLen),
			want: strings.Repeat(`\\`, text.MaxLen),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := pdfreport.EscapeValue(test.in)
			if got != test.want {
				t.Errorf("EscapeValue(%q) = %q, want %q", test.in, got, test.want)
			}

			if len(got) != len(test.want) {
				t.Errorf("EscapeValue(%q) is %d characters, want %d", test.in, len(got), len(test.want))
			}
		})
	}
}

// TestEscapeValueNeverDropsARune checks the escaper's promise on every class of
// rune: a rune of the set the fonts show is kept as itself, and every other rune
// is written as a visible escape, never dropped and never replaced.
func TestEscapeValueNeverDropsARune(t *testing.T) {
	t.Parallel()

	runes := []rune{
		'a', '"', '\\', '\u0000', '\u000a', '\u007f', '\u0080', '\u00a0', '\u00ad', '\u00e9',
		'\u00ff', '\u0100', '\u0416', '\u2028', '\u202e', '\u4e2d', '\uffff', '\U0001f600',
	}

	for _, r := range runes {
		escaped := pdfreport.EscapeValue(string(r))

		if escaped == "" {
			t.Errorf("EscapeValue(%q) dropped the rune", r)

			continue
		}

		if escaped == "?" {
			t.Errorf("EscapeValue(%q) replaced the rune with a question mark", r)
		}

		if winAnsiKept(r) {
			if escaped != string(r) {
				t.Errorf("EscapeValue(%q) = %q, want the rune kept as it is", r, escaped)
			}

			continue
		}

		if !strings.HasPrefix(escaped, `\`) {
			t.Errorf("EscapeValue(%q) = %q, want a visible escape", r, escaped)
		}

		if atoms := escapeAtoms(t, escaped); len(atoms) != 1 || atoms[0] != escaped {
			t.Errorf("EscapeValue(%q) = %q, want one whole escape form", r, escaped)
		}
	}
}

// winAnsiKept reports whether the escaper keeps r as itself: a character the
// core fonts show through WinAnsiEncoding (PRD, Characters), except the
// backslash, the one ASCII character the escaper doubles.
func winAnsiKept(r rune) bool {
	if r == '\\' {
		return false
	}

	if r >= 0x20 && r <= 0x7E {
		return true
	}

	if r >= 0xA1 && r <= 0xFF {
		return r != 0xAD
	}

	return strings.ContainsRune("\u20ac\u201a\u0192\u201e\u2026\u2020\u2021\u02c6\u2030\u0160\u2039\u0152\u017d"+
		"\u2018\u2019\u201c\u201d\u2022\u2013\u2014\u02dc\u2122\u0161\u203a\u0153\u017e\u0178", r)
}

// TestPDFString pins the PDF literal string form: the outer parentheses, the
// escapes of (, ) and \, and a three-digit octal escape for every byte outside
// 0x20 to 0x7E: the WinAnsi bytes of the characters the fonts show, and nothing
// else, since every other character has already become a visible escape.
func TestPDFString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: "()"},
		{name: "plain text", in: "plain text", want: "(plain text)"},
		{name: "parentheses are escaped", in: "a(b)c", want: `(a\(b\)c)`},
		{name: "unbalanced parentheses are escaped", in: "(( )) )", want: `(\(\( \)\) \))`},
		{name: "a backslash is escaped", in: `a\b`, want: `(a\\b)`},
		{name: "a value's own escape stays text", in: `\u0416`, want: `(\\u0416)`},
		{name: "a value's own doubled backslash stays text", in: `\\`, want: `(\\\\)`},
		{name: "a value's own operator stays text", in: ") Tj ET (", want: `(\) Tj ET \()`},
		{name: "the euro sign is its WinAnsi byte", in: "\u20ac", want: `(\200)`},
		{name: "a Latin-1 letter is its WinAnsi byte", in: "\u00e9", want: `(\351)`},
		{name: "a WinAnsi quote is its byte in 0x80 to 0x9F", in: "\u2018", want: `(\221)`},
		{name: "a tab is an octal escape", in: "\u0009", want: `(\011)`},
		{name: "NUL is an octal escape", in: "\u0000", want: `(\000)`},
		{
			name: "a value of every class at once",
			in:   "a(b)c\\d\u00e9\u20ac\u2018",
			want: `(a\(b\)c\\d\351\200\221)`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := pdfreport.PDFString(test.in); got != test.want {
				t.Errorf("PDFString(%q) = %q, want %q", test.in, got, test.want)
			}
		})
	}
}

// TestPDFStringRoundTrips checks the two halves together: the literal string of
// an escaped value, read back by the tests' reader, is that escaped value. This
// is the property every value's drawing relies on.
func TestPDFStringRoundTrips(t *testing.T) {
	t.Parallel()

	values := []string{
		"plain",
		"a(b)c",
		`a\b`,
		"id) Tj ET",
		"\u20ac\u2018\u00e9",
		"\u0416 \u4e2d \U0001f600",
		"x\u00a0y\u00adz",
		strings.Repeat("\u0416", 32),
	}

	for _, value := range values {
		escaped := pdfreport.EscapeValue(value)
		literal := pdfreport.PDFString(escaped)

		lexer := &pdfLexer{data: []byte(literal)}
		read := lexer.parseLiteralString()

		if lexer.pos != len(literal) {
			t.Fatalf("PDFString(%q) is %q, which the reader left at byte %d of %d", escaped, literal, lexer.pos, len(literal))
		}

		shown, err := decodeWinAnsi(read)
		if err != nil {
			t.Fatalf("PDFString(%q) = %q, which does not decode as WinAnsi: %v", escaped, literal, err)
		}

		if shown != escaped {
			t.Errorf("the literal string form of %q reads back as %q", escaped, shown)
		}
	}
}
