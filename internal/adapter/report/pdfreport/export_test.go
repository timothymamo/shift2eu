package pdfreport

// The escaper and the literal string form are unexported: only the writer uses
// them, and no reporter port opens them. The black-box tests reach them through
// these aliases, which exist in test builds alone.

// EscapeValue is escapeValue, for the tests: the character-level escape of a
// value into the displayed ASCII/WinAnsi text, as the PRD's Characters section
// describes it.
var EscapeValue = escapeValue

// PDFString is pdfString, for the tests: the value's PDF literal string form,
// including the outer parentheses.
var PDFString = pdfString
