package site_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/test/site/internal/jsscan"
)

// This file holds the check of the one place the page script handles a
// JavaScript exception: the catch clause around the call of the module's
// function in the run handler. A getter that throws in the options object makes
// shift2euAssess throw, which syscall/js cannot turn into an error the module
// returns (ADR 0036); the exception carries no value the page may show, since
// only code already running in the page can pass such a getter. So the clause
// writes one constant string with textContent and never reads the caught value:
// nothing the exception carries reaches the page, as text or as markup.
//
// The check reads jsscan's tokens, the page script's own scan's lexer, and pairs
// the brackets itself: it needs the catch clause's body as a span of tokens, not
// the script's rule set.

// tryClause is one try statement of a script: its body, its catch clause, and
// the name the clause binds the exception to.
type tryClause struct {
	// guarded holds the try body's tokens, the braces excluded.
	guarded []jsscan.Token
	// caught is the name the catch clause binds the exception to, "" when the
	// clause binds none.
	caught string
	// body holds the catch body's tokens, the braces excluded.
	body []jsscan.Token
	// line is the 1-based line of the catch keyword.
	line int
}

// punctuator reports whether the token at i is the punctuator text.
func punctuator(tokens []jsscan.Token, i int, text string) bool {
	return i >= 0 && i < len(tokens) && tokens[i].Kind == jsscan.Punct && tokens[i].Text == text
}

// bracketPairs maps the index of every bracket to the index of its partner,
// failing the test on a bracket that pairs with none.
func bracketPairs(t *testing.T, name string, tokens []jsscan.Token) map[int]int {
	t.Helper()

	pairs := map[string]string{")": "(", "]": "[", "}": "{"}

	matching := make(map[int]int)

	var stack []int

	for i, token := range tokens {
		if token.Kind != jsscan.Punct {
			continue
		}

		switch token.Text {
		case "(", "[", "{":
			stack = append(stack, i)
		case ")", "]", "}":
			if len(stack) == 0 || tokens[stack[len(stack)-1]].Text != pairs[token.Text] {
				t.Fatalf("%s: %d:%d: %q closes no bracket", name, token.Line, token.Col, token.Text)
			}

			open := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			matching[open] = i
			matching[i] = open
		}
	}

	if len(stack) != 0 {
		token := tokens[stack[len(stack)-1]]
		t.Fatalf("%s: %d:%d: %q is never closed", name, token.Line, token.Col, token.Text)
	}

	return matching
}

// tryClauses returns the try statements of src, each with its catch clause, and
// fails the test on a source it cannot lex or whose brackets do not pair.
func tryClauses(t *testing.T, name string, src []byte) []tryClause {
	t.Helper()

	tokens, err := jsscan.Tokens(src)
	if err != nil {
		t.Fatalf("lex %s: %v", name, err)
	}

	matching := bracketPairs(t, name, tokens)

	var clauses []tryClause

	for i := range tokens {
		if tokens[i].Kind != jsscan.Ident || tokens[i].Text != "try" || !punctuator(tokens, i+1, "{") {
			continue
		}

		close := matching[i+1]
		j := close + 1

		if j >= len(tokens) || tokens[j].Kind != jsscan.Ident || tokens[j].Text != "catch" {
			continue
		}

		clause := tryClause{guarded: tokens[i+2 : close], line: tokens[j].Line}

		k := j + 1

		if punctuator(tokens, k, "(") {
			params := matching[k]

			for _, token := range tokens[k+1 : params] {
				if token.Kind == jsscan.Ident {
					clause.caught = token.Text

					break
				}
			}

			k = params + 1
		}

		if !punctuator(tokens, k, "{") {
			continue
		}

		clause.body = tokens[k+1 : matching[k]]
		clauses = append(clauses, clause)
	}

	return clauses
}

// holdsIdent reports whether tokens holds the identifier text as code.
func holdsIdent(tokens []jsscan.Token, text string) bool {
	for _, token := range tokens {
		if token.Kind == jsscan.Ident && token.Text == text {
			return true
		}
	}

	return false
}

// endOfExpression returns the length of the expression at the start of tokens:
// up to the first ";" outside brackets, or to the end.
func endOfExpression(tokens []jsscan.Token) int {
	depth := 0

	for i, token := range tokens {
		if token.Kind != jsscan.Punct {
			continue
		}

		switch token.Text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
		case ";":
			if depth == 0 {
				return i
			}
		}
	}

	return len(tokens)
}

// tokensText returns the tokens' source text, spaced for a failure message.
func tokensText(tokens []jsscan.Token) string {
	texts := make([]string, 0, len(tokens))

	for _, token := range tokens {
		texts = append(texts, token.Text)
	}

	return strings.Join(texts, " ")
}

// constantCatchFindings returns every way one catch clause breaks the rule: each
// assignment to textContent takes one string literal, the clause shows at least
// one such message, and the caught value is read nowhere.
func constantCatchFindings(clause tryClause) []string {
	var findings []string

	messages := 0

	for i, token := range clause.body {
		if token.Kind != jsscan.Ident || token.Text != "textContent" {
			continue
		}

		if i+1 >= len(clause.body) || !punctuator(clause.body, i-1, ".") || !punctuator(clause.body, i+1, "=") {
			continue
		}

		messages++

		value := clause.body[i+2:]
		value = value[:endOfExpression(value)]

		if len(value) != 1 || value[0].Kind != jsscan.String {
			findings = append(findings, fmt.Sprintf("line %d: the catch clause assigns %q to textContent, want one string literal: a template literal, a concatenation or a value read from the exception shows what the exception carries",
				token.Line, tokensText(value)))
		}
	}

	if messages == 0 {
		findings = append(findings, fmt.Sprintf("line %d: the catch clause assigns nothing to textContent, so the page shows no message when the module throws", clause.line))
	}

	if clause.caught == "" {
		return findings
	}

	for _, token := range clause.body {
		if token.Kind == jsscan.Ident && token.Text == clause.caught {
			findings = append(findings, fmt.Sprintf("line %d: the catch clause reads the caught value %s, want the parameter unused: a hostile getter of the options object chooses the exception, and ADR 0036 has the page show a constant message, never the exception",
				token.Line, clause.caught))

			break
		}
	}

	return findings
}

// runHandlerFindings returns every finding of the catch clauses of src that wrap
// a call of the module's function, and how many such clauses src holds.
func runHandlerFindings(t *testing.T, name string, src []byte) ([]string, int) {
	t.Helper()

	var findings []string

	guarded := 0

	for _, clause := range tryClauses(t, name, src) {
		if !holdsIdent(clause.guarded, "shift2euAssess") {
			continue
		}

		guarded++

		findings = append(findings, constantCatchFindings(clause)...)
	}

	return findings, guarded
}

// TestRunHandlerCatchShowsAConstantMessage pins the page's one catch clause
// (PRD #297's trust section, ADR 0036): the run handler wraps its call of
// shift2euAssess in try/catch, because a getter that throws in the options
// object makes the module throw and syscall/js cannot catch it; the clause writes
// one constant string with textContent and never reads the caught value, so the
// exception can neither reach the page as text nor as markup.
func TestRunHandlerCatchShowsAConstantMessage(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile(filepath.Join(theRepositoryRoot(t), "site", "page.js"))
	if err != nil {
		t.Fatalf("read site/page.js: %v", err)
	}

	findings, guarded := runHandlerFindings(t, "site/page.js", source)

	for _, finding := range findings {
		t.Errorf("site/page.js: %s\nA catch clause shows one constant message with textContent and never reads the caught value (PRD #297, ADR 0036)", finding)
	}

	if guarded != 1 {
		t.Fatalf("site/page.js wraps shift2euAssess in %d try statements with a catch clause, want one: the run handler is the one place a throw from the module is caught", guarded)
	}
}

// TestCatchCheckFires pins that the check above fires, and what it accepts: each
// form in which the exception's value reaches textContent, and a clause that
// shows no message at all, must be reported, while the run handler's own form and
// a catch clause without a parameter must pass. Without this, a check that found
// no catch clause or read no assignment would pass silently.
func TestCatchCheckFires(t *testing.T) {
	t.Parallel()

	const call = `globalThis.shift2euAssess(inventory.value, options);`

	tests := []struct {
		name string
		// body is the catch clause's body.
		body string
		// param is the catch clause's parameter, "" for a clause without one.
		param string
		// want is the part of the finding the check must make, "" for a body
		// the check must accept.
		want string
	}{
		{
			name:  "the caught value assigned to textContent",
			param: "failure",
			body:  `status.textContent = failure;`,
			want:  "want one string literal",
		},
		{
			name:  "the caught value's message assigned",
			param: "e",
			body:  `status.textContent = e.message;`,
			want:  "want one string literal",
		},
		{
			name:  "a template literal using the caught value",
			param: "e",
			body:  "status.textContent = `the assessment failed: ${e.message}`;",
			want:  "want one string literal",
		},
		{
			name:  "a concatenation using the caught value",
			param: "e",
			body:  `status.textContent = "the assessment failed: " + e.message;`,
			want:  "want one string literal",
		},
		{
			name:  "a message from a variable",
			param: "e",
			body:  `status.textContent = message;`,
			want:  "want one string literal",
		},
		{
			name:  "the caught value read outside the message",
			param: "e",
			body:  `lastFailure = e; status.textContent = "The assessment failed unexpectedly.";`,
			want:  "reads the caught value e",
		},
		{
			name:  "no message shown",
			param: "e",
			body:  `return;`,
			want:  "assigns nothing to textContent",
		},
		{
			name:  "the run handler's own form",
			param: "failure",
			body:  `status.textContent = "The assessment failed unexpectedly; the page can run another one."; return;`,
		},
		{
			name: "a catch clause without a parameter",
			body: `status.textContent = "The assessment failed unexpectedly.";`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			param := ""
			if tc.param != "" {
				param = " (" + tc.param + ")"
			}

			src := "try { " + call + " } catch" + param + " { " + tc.body + " }"

			findings, guarded := runHandlerFindings(t, "fixture.js", []byte(src))

			if guarded != 1 {
				t.Fatalf("the check found %d catch clauses around shift2euAssess in %q, want one", guarded, src)
			}

			if tc.want == "" {
				if len(findings) != 0 {
					t.Fatalf("the check reported %v for %q, want no finding", findings, src)
				}

				return
			}

			if len(findings) == 0 {
				t.Fatalf("the check reported nothing for %q, want a finding holding %q", src, tc.want)
			}

			for _, finding := range findings {
				if strings.Contains(finding, tc.want) {
					return
				}
			}

			t.Errorf("the check reported %v for %q, want a finding holding %q", findings, src, tc.want)
		})
	}
}
