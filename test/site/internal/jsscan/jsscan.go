package jsscan

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

// Tokens lexes src into its tokens, in source order, dropping comments and
// white space. It returns an error for a source it cannot lex, such as an
// unterminated string, template literal, regular expression literal or comment.
func Tokens(src []byte) ([]Token, error) {
	_ = src

	panic("not implemented")
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
	_ = src

	panic("not implemented")
}
