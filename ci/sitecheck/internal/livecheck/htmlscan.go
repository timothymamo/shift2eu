package livecheck

import (
	"html"
	"slices"
	"strings"
)

// This file holds the small HTML tokenizer behind Carries and VersionText. It
// reads only what the markers need: start tags with their attributes, end tags
// and text, and it drops what a browser would not render as elements of the
// page: comments, the content of raw-text elements (script, style and their
// kin) and everything inside a <template>. It is no general HTML parser; it
// errs on the side of not finding a marker.

// The kinds of htmlToken.
const (
	tokenText = iota
	tokenStart
	tokenEnd
)

// htmlToken is one token of a page.
type htmlToken struct {
	kind int
	// name is a tag's name in lower case.
	name string
	// attrs are a start tag's attributes, the first of each name, names in
	// lower case and values entity-decoded.
	attrs map[string]string
	// selfClosing says a start tag ended in "/>".
	selfClosing bool
	// text is a text token's text, entity-decoded.
	text string
}

// rawTextElements are the elements whose content is no markup.
var rawTextElements = []string{"script", "style", "xmp", "iframe", "noembed", "noframes", "plaintext"}

// rcdataElements are the elements whose content is text only.
var rcdataElements = []string{"title", "textarea"}

// voidElements are the elements that have no content.
var voidElements = []string{"area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "source", "track", "wbr"}

// tokenizeHTML returns the tokens of page outside comments, raw text and
// templates.
func tokenizeHTML(page string) []htmlToken {
	var (
		tokens        []htmlToken
		templateDepth int
	)

	emit := func(tok htmlToken) {
		if templateDepth == 0 {
			tokens = append(tokens, tok)
		}
	}

	text := func(s string) {
		if s != "" {
			emit(htmlToken{kind: tokenText, text: html.UnescapeString(s)})
		}
	}

	for i := 0; i < len(page); {
		lt := strings.IndexByte(page[i:], '<')
		if lt < 0 {
			text(page[i:])

			break
		}

		text(page[i : i+lt])
		i += lt
		rest := page[i:]

		switch {
		case strings.HasPrefix(rest, "<!--"):
			i += commentLength(rest)
		case strings.HasPrefix(rest, "<!"), strings.HasPrefix(rest, "<?"):
			i += upToGreaterThan(rest)
		case strings.HasPrefix(rest, "</") && len(rest) > 2 && isASCIILetter(rest[2]):
			name, _ := tagName(rest[2:])
			i += upToGreaterThan(rest)

			if name == "template" && templateDepth > 0 {
				templateDepth--

				continue
			}

			emit(htmlToken{kind: tokenEnd, name: name})
		case len(rest) > 1 && isASCIILetter(rest[1]):
			tok, n := startTag(rest)
			i += n

			if tok.name == "template" && !tok.selfClosing {
				templateDepth++

				continue
			}

			emit(tok)

			switch {
			case tok.name == "plaintext":
				i = len(page)
			case slices.Contains(rawTextElements, tok.name):
				i += rawTextLength(page[i:], tok.name)
			case slices.Contains(rcdataElements, tok.name):
				n := rawTextLength(page[i:], tok.name)
				text(page[i : i+n])
				i += n
			}
		default:
			text("<")
			i++
		}
	}

	return tokens
}

// commentLength returns the length of the comment at the start of s, which
// starts with "<!--", or len(s) when it does not end.
func commentLength(s string) int {
	for _, empty := range []string{"<!-->", "<!--->"} {
		if strings.HasPrefix(s, empty) {
			return len(empty)
		}
	}

	end := strings.Index(s[4:], "-->")
	if end < 0 {
		return len(s)
	}

	return 4 + end + 3
}

// upToGreaterThan returns the length of s up to and including its first ">",
// or len(s) when it has none.
func upToGreaterThan(s string) int {
	gt := strings.IndexByte(s, '>')
	if gt < 0 {
		return len(s)
	}

	return gt + 1
}

// rawTextLength returns the length of the raw text at the start of s, up to the
// end tag of name, or len(s) when there is none.
func rawTextLength(s, name string) int {
	lower := strings.ToLower(s)
	closing := "</" + name

	for from := 0; ; {
		at := strings.Index(lower[from:], closing)
		if at < 0 {
			return len(s)
		}

		at += from
		after := at + len(closing)

		if after == len(s) || isTagNameEnd(s[after]) {
			return at
		}

		from = after
	}
}

// startTag reads the start tag at the start of s, which starts with "<" and a
// letter, and returns it and its length. A tag without its closing ">" takes
// the rest of s and yields an empty token name, which matches no element.
func startTag(s string) (htmlToken, int) {
	name, n := tagName(s[1:])
	i := 1 + n
	tok := htmlToken{kind: tokenStart, name: name, attrs: make(map[string]string)}

	for {
		slash := false

		for i < len(s) && (isHTMLSpace(s[i]) || s[i] == '/') {
			slash = s[i] == '/'
			i++
		}

		if i == len(s) {
			return htmlToken{kind: tokenStart}, len(s)
		}

		if s[i] == '>' {
			tok.selfClosing = slash

			return tok, i + 1
		}

		start := i
		i++

		for i < len(s) && !isHTMLSpace(s[i]) && s[i] != '/' && s[i] != '>' && s[i] != '=' {
			i++
		}

		attrName := strings.ToLower(s[start:i])
		value := ""

		for i < len(s) && isHTMLSpace(s[i]) {
			i++
		}

		if i < len(s) && s[i] == '=' {
			i++

			for i < len(s) && isHTMLSpace(s[i]) {
				i++
			}

			if i < len(s) && (s[i] == '"' || s[i] == '\'') {
				end := strings.IndexByte(s[i+1:], s[i])
				if end < 0 {
					return htmlToken{kind: tokenStart}, len(s)
				}

				value = s[i+1 : i+1+end]
				i += end + 2
			} else {
				start = i

				for i < len(s) && !isHTMLSpace(s[i]) && s[i] != '>' {
					i++
				}

				value = s[start:i]
			}
		}

		if _, ok := tok.attrs[attrName]; !ok {
			tok.attrs[attrName] = html.UnescapeString(value)
		}
	}
}

// tagName reads a tag name at the start of s and returns it in lower case and
// its length.
func tagName(s string) (string, int) {
	i := 0
	for i < len(s) && !isTagNameEnd(s[i]) {
		i++
	}

	return strings.ToLower(s[:i]), i
}

// isTagNameEnd reports whether c ends a tag name.
func isTagNameEnd(c byte) bool {
	return isHTMLSpace(c) || c == '/' || c == '>'
}

// isHTMLSpace reports whether c is ASCII white space as HTML defines it.
func isHTMLSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\f' || c == '\r'
}

// isASCIILetter reports whether c is an ASCII letter.
func isASCIILetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// markedElement is what findMarked found for one id.
type markedElement struct {
	// count is the number of elements with the id.
	count int
	// hidden says the one element has a hidden attribute.
	hidden bool
	// text is the one element's text content, entity-decoded, untrimmed.
	text string
}

// findMarked finds the elements of page whose id attribute is exactly id and,
// when there is exactly one, its hidden attribute and its text content.
func findMarked(page []byte, id string) markedElement {
	tokens := tokenizeHTML(string(page))

	var (
		found markedElement
		at    int
	)

	for i, tok := range tokens {
		if tok.kind != tokenStart {
			continue
		}

		if value, ok := tok.attrs["id"]; ok && value == id {
			found.count++
			at = i
		}
	}

	if found.count != 1 {
		return found
	}

	element := tokens[at]
	_, found.hidden = element.attrs["hidden"]

	if element.selfClosing || slices.Contains(voidElements, element.name) {
		return found
	}

	var text strings.Builder

	depth := 0

	for _, tok := range tokens[at+1:] {
		switch {
		case tok.kind == tokenText:
			text.WriteString(tok.text)
		case tok.kind == tokenStart && tok.name == element.name && !tok.selfClosing:
			depth++
		case tok.kind == tokenEnd && tok.name == element.name:
			if depth == 0 {
				found.text = text.String()

				return found
			}

			depth--
		}
	}

	found.text = text.String()

	return found
}
