package jsscan

import (
	"errors"
	"fmt"
)

// Kind is the kind of a JavaScript token.
type Kind int

// The kinds of token Tokens returns, in the order it declares them. A comment
// is no token: the lexer drops comments and white space, so a banned name in a
// comment is no finding.
const (
	// Ident is an identifier or a keyword.
	Ident Kind = iota
	// Punct is punctuation or an operator: "(", ";", "!==", "=>", "...", each
	// the longest operator at that position.
	Punct
	// Number is a numeric literal.
	Number
	// String is a quoted string literal, its quotes included in Text.
	String
	// Template is one text chunk of a template literal, its delimiters
	// included in Text: "`a${" for a head, "}b${" for a middle chunk, "}c`" for
	// a tail, and the whole "`abc`" when the literal holds no expression. The
	// expression between two chunks is lexed as ordinary tokens, so a banned
	// name inside `${...}` is still a token.
	Template
	// Regex is a regular expression literal, its slashes and flags included in
	// Text.
	Regex
)

// Token is one JavaScript token.
type Token struct {
	// Kind is the token's kind.
	Kind Kind
	// Text is the token's source text. For String its quotes, for Template its
	// delimiters and for Regex its slashes and flags are included; a Template
	// chunk holds its own delimiters and no delimiters of the chunks beside it.
	Text string
	// Line is the 1-based line of the token's first byte.
	Line int
	// Col is the 1-based column of the token's first byte, counted in bytes.
	Col int
}

// ErrLex is returned, wrapped, by Tokens and Scan for a source the lexer cannot
// read.
var ErrLex = errors.New("cannot lex the script")

// punctuators are the operators and punctuation the lexer reads, longest
// first, so the longest operator at a position wins.
var punctuators = []string{
	">>>=",
	"...", "===", "!==", "**=", "<<=", ">>=", ">>>", "&&=", "||=", "??=",
	"=>", "==", "!=", "<=", ">=", "&&", "||", "??", "?.", "++", "--",
	"+=", "-=", "*=", "/=", "%=", "&=", "|=", "^=", "**", "<<", ">>",
	"{", "}", "(", ")", "[", "]", ";", ",", "<", ">", "+", "-", "*", "/",
	"%", "&", "|", "^", "!", "~", "?", ":", "=", ".",
}

// keywordsBeforeExpression are the keywords after which an expression starts,
// so a slash after them starts a regular expression literal and a bracket an
// array literal, not a division or a computed member access.
var keywordsBeforeExpression = map[string]bool{
	"return": true, "typeof": true, "instanceof": true, "in": true, "of": true,
	"new": true, "delete": true, "void": true, "throw": true, "case": true,
	"do": true, "else": true, "yield": true, "await": true,
	"const": true, "let": true, "var": true,
}

// lexer is the state of one Tokens call.
type lexer struct {
	src    []byte
	pos    int
	line   int
	col    int
	tokens []Token
	// braces holds one entry per open brace: true for the "${" that opens a
	// template literal's expression, false for a "{" of the code.
	braces []bool
}

// Tokens lexes src into its tokens, in source order, dropping comments and
// white space. It returns an error for a source it cannot lex, such as an
// unterminated string, template literal, regular expression literal or comment.
//
// It reads ASCII code only: a byte outside ASCII is allowed in a comment, a
// string, a template's text and a regular expression, and is an error
// anywhere else, as is a backslash outside them (an escaped identifier such as
// fetch names fetch without spelling it).
func Tokens(src []byte) ([]Token, error) {
	l := &lexer{src: src, line: 1, col: 1}

	for l.pos < len(l.src) {
		if err := l.next(); err != nil {
			return nil, err
		}
	}

	if len(l.braces) != 0 {
		if l.braces[len(l.braces)-1] {
			return nil, l.errorf("an unterminated template literal expression")
		}

		return nil, l.errorf("an unclosed {")
	}

	return l.tokens, nil
}

// errorf returns a lexing error at the lexer's position.
func (l *lexer) errorf(format string, args ...any) error {
	return fmt.Errorf("%w: %d:%d: %s", ErrLex, l.line, l.col, fmt.Sprintf(format, args...))
}

// advance moves the position n bytes on, counting lines and columns.
func (l *lexer) advance(n int) {
	for range n {
		if l.src[l.pos] == '\n' {
			l.line++
			l.col = 1
		} else {
			l.col++
		}

		l.pos++
	}
}

// emit appends a token of kind for the n bytes at the position, and moves past
// them.
func (l *lexer) emit(kind Kind, n int) {
	l.tokens = append(l.tokens, Token{Kind: kind, Text: string(l.src[l.pos : l.pos+n]), Line: l.line, Col: l.col})
	l.advance(n)
}

// peek returns the byte at offset from the position, or 0 past the end.
func (l *lexer) peek(offset int) byte {
	if l.pos+offset < len(l.src) {
		return l.src[l.pos+offset]
	}

	return 0
}

// next reads one token, comment or run of white space at the position.
func (l *lexer) next() error {
	c := l.src[l.pos]

	switch {
	case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\v' || c == '\f':
		l.advance(1)

		return nil
	case c == '/' && l.peek(1) == '/':
		n := 2
		for l.pos+n < len(l.src) && l.src[l.pos+n] != '\n' {
			n++
		}

		l.advance(n)

		return nil
	case c == '/' && l.peek(1) == '*':
		for n := 2; l.pos+n+1 < len(l.src); n++ {
			if l.src[l.pos+n] == '*' && l.src[l.pos+n+1] == '/' {
				l.advance(n + 2)

				return nil
			}
		}

		return l.errorf("an unterminated comment")
	case c == '/' && l.regexAllowed():
		return l.regex()
	case c == '"' || c == '\'':
		return l.quoted(c)
	case c == '`':
		return l.template(1, Template)
	case c == '}' && len(l.braces) > 0 && l.braces[len(l.braces)-1]:
		l.braces = l.braces[:len(l.braces)-1]

		return l.template(1, Template)
	case isDigit(c) || (c == '.' && isDigit(l.peek(1))):
		l.number()

		return nil
	case isIdentStart(c):
		n := 1
		for l.pos+n < len(l.src) && isIdentPart(l.src[l.pos+n]) {
			n++
		}

		l.emit(Ident, n)

		return nil
	}

	return l.punct()
}

// punct reads one operator or punctuator, tracking braces.
func (l *lexer) punct() error {
	rest := l.src[l.pos:]

	for _, p := range punctuators {
		if len(rest) < len(p) || string(rest[:len(p)]) != p {
			continue
		}

		// "?." followed by a digit is a conditional and a number: a ? .5 : b.
		if p == "?." && isDigit(l.peek(2)) {
			continue
		}

		switch p {
		case "{":
			l.braces = append(l.braces, false)
		case "}":
			if len(l.braces) == 0 {
				return l.errorf("a } closes no {")
			}

			l.braces = l.braces[:len(l.braces)-1]
		}

		l.emit(Punct, len(p))

		return nil
	}

	if l.src[l.pos] >= 0x80 {
		return l.errorf("a byte outside ASCII in the code, 0x%02x: the page script's code is ASCII", l.src[l.pos])
	}

	return l.errorf("an unexpected character %q", l.src[l.pos])
}

// quoted reads a string literal opened by quote.
func (l *lexer) quoted(quote byte) error {
	for n := 1; l.pos+n < len(l.src); n++ {
		switch l.src[l.pos+n] {
		case '\\':
			// An escape, a line continuation written \ CR LF included.
			if l.pos+n+2 < len(l.src) && l.src[l.pos+n+1] == '\r' && l.src[l.pos+n+2] == '\n' {
				n++
			}

			n++
		case '\n':
			return l.errorf("an unterminated string literal")
		case quote:
			l.emit(String, n+1)

			return nil
		}
	}

	return l.errorf("an unterminated string literal")
}

// template reads one chunk of a template literal: from the position, where
// the chunk's opening delimiter of length open is, to its closing backtick or
// to the "${" that opens an expression.
func (l *lexer) template(open int, kind Kind) error {
	for n := open; l.pos+n < len(l.src); n++ {
		switch l.src[l.pos+n] {
		case '\\':
			n++
		case '`':
			l.emit(kind, n+1)

			return nil
		case '$':
			if l.pos+n+1 < len(l.src) && l.src[l.pos+n+1] == '{' {
				l.emit(kind, n+2)
				l.braces = append(l.braces, true)

				return nil
			}
		}
	}

	return l.errorf("an unterminated template literal")
}

// regex reads a regular expression literal and its flags.
func (l *lexer) regex() error {
	inClass := false

	for n := 1; l.pos+n < len(l.src); n++ {
		switch l.src[l.pos+n] {
		case '\\':
			n++
		case '\n':
			return l.errorf("an unterminated regular expression literal")
		case '[':
			inClass = true
		case ']':
			inClass = false
		case '/':
			if inClass {
				continue
			}

			n++
			for l.pos+n < len(l.src) && isIdentPart(l.src[l.pos+n]) {
				n++
			}

			l.emit(Regex, n)

			return nil
		}
	}

	return l.errorf("an unterminated regular expression literal")
}

// number reads a numeric literal: digits, letters (hexadecimal digits, an
// exponent, a BigInt suffix), dots, separators and an exponent's sign.
func (l *lexer) number() {
	hex := l.src[l.pos] == '0' && (l.peek(1) == 'x' || l.peek(1) == 'X')

	n := 1

	for l.pos+n < len(l.src) {
		c := l.src[l.pos+n]

		if isIdentPart(c) || c == '.' {
			n++

			continue
		}

		if (c == '+' || c == '-') && !hex && (l.src[l.pos+n-1] == 'e' || l.src[l.pos+n-1] == 'E') {
			n++

			continue
		}

		break
	}

	l.emit(Number, n)
}

// regexAllowed reports whether a slash at the position starts a regular
// expression literal: whether the token before it ends an expression or not.
func (l *lexer) regexAllowed() bool {
	if len(l.tokens) == 0 {
		return true
	}

	prev := l.tokens[len(l.tokens)-1]

	switch prev.Kind {
	case Ident:
		return keywordsBeforeExpression[prev.Text]
	case Number, String, Regex:
		return false
	case Template:
		return prev.Text[len(prev.Text)-1] != '`'
	case Punct:
		switch prev.Text {
		case ")", "]", "}", "++", "--":
			return false
		}
	}

	return true
}

// isDigit reports whether c is an ASCII digit.
func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

// isIdentStart reports whether c starts an identifier: ASCII only.
func isIdentStart(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// isIdentPart reports whether c continues an identifier.
func isIdentPart(c byte) bool {
	return isIdentStart(c) || isDigit(c)
}

// Violation is one breach of the page-script rules.
type Violation struct {
	// Line is the 1-based line of the token the breach is at.
	Line int
	// Col is the 1-based column of that token, counted in bytes.
	Col int
	// Message says what is wrong and names the construct, so a failure names
	// the rule that fired and the token it fired on.
	Message string
}

// Scan returns every breach of the page-script rules in src, in source order,
// and no violation for a script that holds none.
//
// It fails closed: src it cannot lex is an error, not a pass.
func Scan(src []byte) ([]Violation, error) {
	tokens, err := Tokens(src)
	if err != nil {
		return nil, err
	}

	s, err := newScanner(tokens)
	if err != nil {
		return nil, err
	}

	s.run()

	return s.sorted(), nil
}
