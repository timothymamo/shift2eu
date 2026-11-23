package site_test

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
)

// This file holds the HTML reader the site's static checks use: a tokenizer for
// the subset of HTML the built pages and the HTML reporter's goldens are made
// of (elements, attributes, text, comments, the raw-text elements script and
// style), and the queries on it. It is no HTML parser: it does not repair a
// broken document, it does not run the tree-construction rules, and it does not
// know every void element's behaviour beyond the list below. It is enough to
// read a tag, its attributes and its text content, which is what the checks
// need, and it fails on a document it cannot read rather than guessing.

// attribute is one HTML attribute.
type attribute struct {
	// name is the attribute's name, lower case.
	name string
	// value is the attribute's value as the source spells it, entities
	// included.
	value string
	// valued reports whether the attribute has a value: `hidden` has none,
	// `hidden=""` has an empty one.
	valued bool
}

// element is one HTML element of a parsed document.
type element struct {
	// tag is the element's name, lower case.
	tag string
	// attrs are the element's attributes, in source order.
	attrs []attribute
	// children are the element's child elements, in source order.
	children []*element
	// parent is the enclosing element, nil for the document.
	parent *element
	// text is the element's own text: the runs of text between its start tag
	// and its first child element, and between its child elements, as the
	// source spells it.
	text string
	// texts are those runs, each with the child it comes before: they are what
	// keeps the element's text content in document order.
	texts []textRun
}

// textRun is one run of an element's own text, and the child element it comes
// before: at is the index in the element's children of the child the run
// precedes, and the length of children for the run after the last one.
type textRun struct {
	at   int
	text string
}

// addText appends a run of the element's own text: a run that directly follows
// the previous one joins it, so a text interrupted by no child stays one run.
func (e *element) addText(text string) {
	e.text += text

	if n := len(e.texts); n > 0 && e.texts[n-1].at == len(e.children) {
		e.texts[n-1].text += text

		return
	}

	e.texts = append(e.texts, textRun{at: len(e.children), text: text})
}

// voidElements are the elements with no content and no end tag.
var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true,
	"hr": true, "img": true, "input": true, "link": true, "meta": true,
	"param": true, "source": true, "track": true, "wbr": true,
}

// rawTextElements are the elements whose content is text, not markup.
var rawTextElements = map[string]bool{"script": true, "style": true}

// document is a parsed HTML document.
type document struct {
	// nodes are every element of the document, in source order.
	nodes []*element
}

// parseHTML reads src as a document, failing the test when it cannot.
func parseHTML(t *testing.T, name string, src []byte) *document {
	t.Helper()

	top := &element{tag: "#document"}
	stack := []*element{top}
	nodes := make([]*element, 0, 32)

	i := 0
	for i < len(src) {
		if src[i] != '<' {
			end := bytes.IndexByte(src[i:], '<')
			if end < 0 {
				end = len(src) - i
			}

			stack[len(stack)-1].addText(string(src[i : i+end]))

			i += end

			continue
		}

		rest := src[i:]

		switch {
		case bytes.HasPrefix(rest, []byte("<!--")):
			end := bytes.Index(rest, []byte("-->"))
			if end < 0 {
				t.Fatalf("%s: an unterminated comment at byte %d", name, i)
			}

			i += end + len("-->")
		case bytes.HasPrefix(rest, []byte("<!")):
			// A doctype or another declaration: skip it.
			end := bytes.IndexByte(rest, '>')
			if end < 0 {
				t.Fatalf("%s: an unterminated declaration at byte %d", name, i)
			}

			i += end + 1
		case bytes.HasPrefix(rest, []byte("</")):
			consumed, tag := readEndTag(t, name, rest)

			i += consumed

			for j := len(stack) - 1; j > 0; j-- {
				if stack[j].tag == tag {
					stack = stack[:j]

					break
				}
			}
		case bytes.HasPrefix(rest, []byte("<?")):
			end := bytes.IndexByte(rest, '>')
			if end < 0 {
				t.Fatalf("%s: an unterminated processing instruction at byte %d", name, i)
			}

			i += end + 1
		default:
			consumed, el, selfClosing := readStartTag(t, name, rest)

			i += consumed

			parent := stack[len(stack)-1]
			el.parent = parent
			parent.children = append(parent.children, el)
			nodes = append(nodes, el)

			if selfClosing || voidElements[el.tag] {
				continue
			}

			if rawTextElements[el.tag] {
				text, length := readRawText(t, name, el.tag, src[i:])

				el.addText(text)

				i += length
				// Consume the element's own end tag.
				consumed, _ := readEndTag(t, name, src[i:])

				i += consumed

				continue
			}

			stack = append(stack, el)
		}
	}

	return &document{nodes: nodes}
}

// readStartTag reads one start tag at the front of src and returns the bytes it
// consumed, the element and whether it closed itself.
func readStartTag(t *testing.T, name string, src []byte) (int, *element, bool) {
	t.Helper()

	if src[0] != '<' {
		t.Fatalf("%s: a start tag must begin with '<'", name)
	}

	i := 1
	start := i

	for i < len(src) && !isHTMLSpace(src[i]) && src[i] != '>' && src[i] != '/' {
		i++
	}

	if i == start {
		t.Fatalf("%s: a start tag with no name at byte %d", name, 0)
	}

	el := &element{tag: strings.ToLower(string(src[start:i]))}
	selfClosing := false

	for {
		for i < len(src) && isHTMLSpace(src[i]) {
			i++
		}

		if i >= len(src) {
			t.Fatalf("%s: the start tag <%s> never ends", name, el.tag)
		}

		if src[i] == '>' {
			i++

			break
		}

		if src[i] == '/' && i+1 < len(src) && src[i+1] == '>' {
			selfClosing = true
			i += 2

			break
		}

		attrStart := i

		for i < len(src) && !isHTMLSpace(src[i]) && src[i] != '=' && src[i] != '>' && src[i] != '/' {
			i++
		}

		if i == attrStart {
			t.Fatalf("%s: a name-less attribute in <%s> at byte %d", name, el.tag, i)
		}

		attr := attribute{name: strings.ToLower(string(src[attrStart:i]))}

		for i < len(src) && isHTMLSpace(src[i]) {
			i++
		}

		if i < len(src) && src[i] == '=' {
			i++

			for i < len(src) && isHTMLSpace(src[i]) {
				i++
			}

			if i >= len(src) {
				t.Fatalf("%s: the attribute %s of <%s> has no value", name, attr.name, el.tag)
			}

			attr.valued = true

			if src[i] == '"' || src[i] == '\'' {
				quote := src[i]
				i++

				valueStart := i

				for i < len(src) && src[i] != quote {
					i++
				}

				if i >= len(src) {
					t.Fatalf("%s: the value of the attribute %s of <%s> is unterminated", name, attr.name, el.tag)
				}

				attr.value = string(src[valueStart:i])
				i++
			} else {
				valueStart := i

				for i < len(src) && !isHTMLSpace(src[i]) && src[i] != '>' {
					i++
				}

				attr.value = string(src[valueStart:i])
			}
		}

		el.attrs = append(el.attrs, attr)
	}

	return i, el, selfClosing
}

// readEndTag reads one end tag at the front of src and returns the bytes it
// consumed and the tag name it names.
func readEndTag(t *testing.T, name string, src []byte) (int, string) {
	t.Helper()

	if !bytes.HasPrefix(src, []byte("</")) {
		t.Fatalf("%s: an end tag must begin with '</'", name)
	}

	end := bytes.IndexByte(src, '>')
	if end < 0 {
		t.Fatalf("%s: an end tag never ends", name)
	}

	tag := strings.ToLower(strings.TrimSpace(string(src[2:end])))

	return end + 1, tag
}

// readRawText reads the content of a raw-text element up to its own end tag,
// returning the text and the bytes it consumed.
func readRawText(t *testing.T, name, tag string, src []byte) (string, int) {
	t.Helper()

	closing := "</" + tag
	lower := bytes.ToLower(src)

	for i := 0; i+len(closing) <= len(lower); i++ {
		if bytes.HasPrefix(lower[i:], []byte(closing)) {
			return string(src[:i]), i
		}
	}

	t.Fatalf("%s: the <%s> element has no end tag", name, tag)

	return "", 0
}

// isHTMLSpace reports whether b is white space in HTML.
func isHTMLSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f'
}

// all returns every element of the document, in source order.
func (d *document) all() []*element {
	return d.nodes
}

// find returns every element whose tag is tag, in source order.
func (d *document) find(tag string) []*element {
	var out []*element

	for _, el := range d.nodes {
		if el.tag == tag {
			out = append(out, el)
		}
	}

	return out
}

// body returns the document's <body> element, failing the test when it holds
// none or more than one.
func (d *document) body(t *testing.T) *element {
	t.Helper()

	bodies := d.find("body")
	if len(bodies) != 1 {
		t.Fatalf("the document holds %d <body> elements, want exactly one", len(bodies))
	}

	return bodies[0]
}

// head returns the document's <head> element, failing the test when it holds
// none or more than one.
func (d *document) head(t *testing.T) *element {
	t.Helper()

	heads := d.find("head")
	if len(heads) != 1 {
		t.Fatalf("the document holds %d <head> elements, want exactly one", len(heads))
	}

	return heads[0]
}

// attr returns the value of the attribute name, which is name when the
// attribute has no value at all.
func (e *element) attr(name string) (string, bool) {
	for _, attr := range e.attrs {
		if attr.name == name {
			return attr.value, true
		}
	}

	return "", false
}

// hasAttr reports whether the element carries the attribute name.
func (e *element) hasAttr(name string) bool {
	_, ok := e.attr(name)

	return ok
}

// ancestors returns the element's ancestors, nearest first, up to the document
// node.
func (e *element) ancestors() []*element {
	var out []*element

	for node := e.parent; node != nil; node = node.parent {
		out = append(out, node)
	}

	return out
}

// textContent returns the element's text content: the text of every descendant
// text node, concatenated and entity-decoded, as the browser's textContent
// reads it for a document without comments in its text.
func (e *element) textContent() string {
	var b strings.Builder

	collectText(e, &b)

	return htmlUnescape(b.String())
}

// collectText appends the text of el and its descendants to b, raw, in the
// order the document holds it: the element's own runs of text in among the text
// of its children, as a browser's textContent reads it.
func collectText(el *element, b *strings.Builder) {
	next := 0

	for i, child := range el.children {
		for ; next < len(el.texts) && el.texts[next].at <= i; next++ {
			b.WriteString(el.texts[next].text)
		}

		collectText(child, b)
	}

	for ; next < len(el.texts); next++ {
		b.WriteString(el.texts[next].text)
	}
}

// htmlUnescape decodes the character references the built pages and the
// HTML reporter's goldens use.
func htmlUnescape(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}

	var b strings.Builder

	for i := 0; i < len(s); i++ {
		if s[i] != '&' {
			b.WriteByte(s[i])

			continue
		}

		end := strings.IndexByte(s[i:], ';')
		if end < 0 {
			b.WriteByte(s[i])

			continue
		}

		entity := s[i+1 : i+end]

		if decoded, ok := decodeEntity(entity); ok {
			b.WriteString(decoded)

			i += end

			continue
		}

		b.WriteByte(s[i])
	}

	return b.String()
}

// decodeEntity decodes one entity name, with or without its numeric form.
func decodeEntity(entity string) (string, bool) {
	switch entity {
	case "amp":
		return "&", true
	case "lt":
		return "<", true
	case "gt":
		return ">", true
	case "quot":
		return `"`, true
	case "apos":
		return "'", true
	case "nbsp":
		return "\u00a0", true
	}

	digits := ""
	base := 10

	switch {
	case strings.HasPrefix(entity, "#x"), strings.HasPrefix(entity, "#X"):
		digits, base = entity[2:], 16
	case strings.HasPrefix(entity, "#"):
		digits = entity[1:]
	default:
		return "", false
	}

	code, err := strconv.ParseInt(digits, base, 32)
	if err != nil || code <= 0 || code > 0x10ffff {
		return "", false
	}

	return string(rune(code)), true
}
