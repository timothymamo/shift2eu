//go:build sitefront && !js

package sitefront_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// This file holds the behaviour test's reading of infra/site/container.tf, the
// one place the front's image and its command line live (docs/adr/0045, *The
// image and its configuration*): the image the container reads (the local the
// Renovate comment pins, and nothing else) and the command and args the
// container runs, with the Caddyfile's bytes in the one Terraform's file()
// fills in. The behaviour test then runs exactly what
// the platform will run, so a change of the container's start command is held to
// the front's answers, and the image pin is read here rather than copied a
// second time.

// containerPath is the container's file from the repository root.
const containerPath = "infra/site/container.tf"

// imagePinComment is the Renovate comment above the image reference
// (CLAUDE.md rule 7): the one line that says which image the next line pins.
const imagePinComment = "datasource=docker depName=caddy"

// containerRun is what infra/site/container.tf runs: the image, and the command
// line the platform starts in it, as `image command args...` takes them.
type containerRun struct {
	image string
	argv  []string
}

// readContainerRun reads infra/site/container.tf into the image and the command
// line the container runs, with file("${path.module}/Caddyfile") replaced by the
// Caddyfile's bytes.
func readContainerRun() (containerRun, error) {
	root, err := repositoryRoot()
	if err != nil {
		return containerRun{}, err
	}

	dir := filepath.Join(root, "infra", "site")

	src, err := os.ReadFile(filepath.Join(dir, "container.tf"))
	if err != nil {
		return containerRun{}, fmt.Errorf("read %s: %w: the front's image and start command live there, and the behaviour test runs what the platform runs (docs/adr/0045)", containerPath, err)
	}

	caddyfile, err := os.ReadFile(filepath.Join(dir, "Caddyfile"))
	if err != nil {
		return containerRun{}, fmt.Errorf("read infra/site/Caddyfile: %w: the container runs it byte for byte (docs/adr/0045)", err)
	}

	return containerRunFrom(string(src), string(caddyfile))
}

// containerRunFrom reads a container.tf's text, file() calls taking caddyfile as
// the Caddyfile's contents.
func containerRunFrom(src, caddyfile string) (containerRun, error) {
	image, err := imageReference(src)
	if err != nil {
		return containerRun{}, err
	}

	command, err := hclList(src, "command")
	if err != nil {
		return containerRun{}, fmt.Errorf("%s: %w", containerPath, err)
	}

	args, err := hclList(src, "args")
	if err != nil {
		return containerRun{}, fmt.Errorf("%s: %w", containerPath, err)
	}

	argv := make([]string, 0, len(command)+len(args))

	for _, element := range command {
		value, err := hclElementValue(element)
		if err != nil {
			return containerRun{}, fmt.Errorf("%s: command's element %s: %w", containerPath, element, err)
		}
		argv = append(argv, value)
	}

	for _, element := range args {
		if path, ok := hclFileCall(element); ok {
			if path != "${path.module}/Caddyfile" {
				return containerRun{}, fmt.Errorf("%s: args: file(%q), want file(\"${path.module}/Caddyfile\"): the container writes the front's Caddyfile (docs/adr/0045)", containerPath, path)
			}
			argv = append(argv, caddyfile)

			continue
		}

		value, err := hclElementValue(element)
		if err != nil {
			return containerRun{}, fmt.Errorf("%s: args' element %s: %w", containerPath, element, err)
		}
		argv = append(argv, value)
	}

	return containerRun{image: image, argv: argv}, nil
}

// hclElementValue returns what a quoted list element gives the container: the string the quotes hold, with
// HCL's own escapes decoded - \n \r \t \" \\ \uNNNN and \UNNNNNNNN, and no others. An element that carries a
// ${ or %{ template is refused: the reader runs no template, so it would pass the template itself to the
// container instead of what the platform runs (docs/adr/0045). An element that holds a template escape ($${ or
// %%{, which HCL decodes to a literal ${ or %{) or an escape that decodes to a $ or a % is refused too, fail
// closed: the reader does not compare the value it would run with the one the platform gets. An escape HCL has
// and Go also has as another character (\x41, \a, the octal \101) is refused as well: tofu would refuse the
// configuration, and a reader that decoded them would run a command line the configuration does not hold.
func hclElementValue(element string) (string, error) {
	if err := checkHCLEscapes(element); err != nil {
		return "", err
	}

	for _, escape := range []string{"$${", "%%{"} {
		if strings.Contains(element, escape) {
			return "", fmt.Errorf("the element holds %s, HCL's escape for a literal %s: this reader refuses it in a start command, fail closed, rather than compare the value it would run with the one the platform gets (docs/adr/0045)", escape, escape[1:])
		}
	}

	value, err := strconv.Unquote(element)
	if err != nil {
		return "", err
	}

	// A second guard: hclStringEnd already refuses a raw ${ and %{, and the refusals above every escape of
	// them, but a raw $ or % before an escape that decodes to { ("a${") still decodes to one here.
	for _, c := range []byte{'$', '%'} {
		if strings.Contains(value, string(c)+"{") {
			what := "interpolation"
			if c == '%' {
				what = "directive"
			}

			return "", fmt.Errorf("the element decodes to %c{, which starts an HCL template (%s): this reader does not run templates (docs/adr/0045)", c, what)
		}
	}

	return value, nil
}

// checkHCLEscapes refuses a backslash escape in a quoted element that HCL does not have: HCL's are \n \r \t
// \" \\ \uNNNN and \UNNNNNNNN, while strconv.Unquote also decodes Go's \x41, \a, \b, \f, \v and the octal
// \101, which tofu would refuse. A \u or \U escape that decodes to a $ or a % is refused too, fail closed: it
// spells the start of a template, or of its escape, in a way this reader would read one way and HCL another.
func checkHCLEscapes(element string) error {
	for i := 0; i < len(element); i++ {
		if element[i] != '\\' {
			continue
		}

		if i+1 >= len(element) {
			return fmt.Errorf("a \\ ends the element: HCL's escapes are \\n \\r \\t \\\" \\\\ \\uNNNN and \\UNNNNNNNN")
		}

		switch c := element[i+1]; {
		case c == 'n', c == 'r', c == 't', c == '"', c == '\\':
			i++
		case c == 'u' && hexDigits(element, i+2, 4), c == 'U' && hexDigits(element, i+2, 8):
			n := 4
			if c == 'U' {
				n = 8
			}

			code, err := strconv.ParseUint(element[i+2:i+2+n], 16, 32)
			if err != nil {
				return err
			}

			if code == '$' || code == '%' {
				return fmt.Errorf("%s decodes to %c, which this reader refuses in a start command, fail closed: write no escape of $ or %% there (docs/adr/0045)", element[i:i+2+n], rune(code))
			}

			i += 1 + n
		default:
			end := i + 2
			if c == 'x' || c >= '0' && c <= '7' {
				end = min(i+4, len(element))
			}

			return fmt.Errorf("%s is no HCL escape (HCL's are \\n \\r \\t \\\" \\\\ \\uNNNN and \\UNNNNNNNN): tofu would refuse it, and decoding it as Go does would run a command line the configuration does not hold (docs/adr/0045)", element[i:end])
		}
	}

	return nil
}

// hexDigits reports whether s holds n hex digits from offset i.
func hexDigits(s string, i, n int) bool {
	if i+n > len(s) {
		return false
	}

	for _, c := range []byte(s[i : i+n]) {
		if !strings.ContainsRune("0123456789abcdefABCDEF", rune(c)) {
			return false
		}
	}

	return true
}

// imageReference returns the image reference src pins, read from the tokens of the file: the site container's
// image attribute, which must be exactly local.caddy_image, and that local, which a top-level locals block sets
// once and the Renovate comment's own line pins, and whose reference must name a digest, so the platform runs one
// immutable image (docs/adr/0045). A pin a /* */ comment holds is no pin: a commented-out line is no line the
// platform reads.
func imageReference(src string) (string, error) {
	tokens, comments, err := hclScan(src)
	if err != nil {
		return "", err
	}

	body, err := containerBody(tokens)
	if err != nil {
		return "", err
	}

	if err := checkContainerImage(body); err != nil {
		return "", err
	}

	pin, err := caddyImagePin(tokens)
	if err != nil {
		return "", err
	}

	if !renovatePinned(src, comments, tokens[pin-2].start) {
		return "", fmt.Errorf("%s: caddy_image on line %d has no Renovate comment on the line above it: no line carries %q directly above the local, outside a /* */ comment (CLAUDE.md rule 7, docs/adr/0045)", containerPath, lineOf(src, tokens[pin-2].start), renovateComment)
	}

	text := tokens[pin].text
	if strings.Contains(text, `\`) {
		return "", fmt.Errorf("%s: the image %s holds an escape: an image reference is written plainly (docs/adr/0045)", containerPath, text)
	}

	image, err := strconv.Unquote(text)
	if err != nil {
		return "", fmt.Errorf("%s: unquote the image %s: %w", containerPath, text, err)
	}

	_, digest, ok := strings.Cut(image, "@sha256:")
	if !ok {
		return "", fmt.Errorf("%s: the image is %q, want a reference pinned by digest (name:tag@sha256:...): a tag alone can be moved under the platform (docs/adr/0045)", containerPath, image)
	}

	if !isImageDigest(digest) {
		return "", fmt.Errorf("%s: the image is %q, want @sha256: followed by exactly 64 lower-case hex characters and nothing after them: the digest names the one immutable image the platform runs (docs/adr/0045)", containerPath, image)
	}

	return image, nil
}

// checkContainerImage holds the site container's image attribute, in its body, to exactly the tokens
// local . caddy_image, and nothing after them.
func checkContainerImage(body []hclToken) error {
	j, err := attrValue(body, "image")
	if err != nil {
		return fmt.Errorf("%s: %w", containerPath, err)
	}

	want := []string{"local", ".", "caddy_image"}
	for k, text := range want {
		if j+k >= len(body) || body[j+k].text != text {
			return fmt.Errorf("%s: the site container's image is not exactly %s: a literal or another local is a reference the Renovate comment does not move (docs/adr/0045)", containerPath, caddyImageLocal)
		}
	}

	if !valueEnds(body, j+len(want)) {
		return fmt.Errorf("%s: an expression follows %s in the site container's image: the platform runs the expression's value (docs/adr/0045)", containerPath, caddyImageLocal)
	}

	return nil
}

// caddyImagePin returns the index in tokens of the string a top-level locals block sets caddy_image to: set
// once across every locals block, as one quoted string and nothing after it. The tokens two and one before it
// are the name caddy_image and its =.
func caddyImagePin(tokens []hclToken) (int, error) {
	pin, found := -1, 0

	for i, depth := 0, 0; i < len(tokens); i++ {
		tok := tokens[i]

		switch {
		case tok.kind == tokPunct && tok.text == "{":
			depth++
		case tok.kind == tokPunct && tok.text == "}":
			depth--
		case depth == 0 && tok.kind == tokIdent && tok.text == "locals" &&
			i+1 < len(tokens) && tokens[i+1].kind == tokPunct && tokens[i+1].text == "{":
			body, end := blockBody(tokens, i+1)

			for _, j := range attrValues(body, "caddy_image") {
				found++
				pin = i + 2 + j
				if j >= len(body) || body[j].kind != tokString || !valueEnds(body, j+1) {
					return 0, fmt.Errorf("%s: caddy_image is not one quoted image reference: the Renovate comment pins one string (docs/adr/0045)", containerPath)
				}
			}

			i = end
		}
	}

	switch found {
	case 0:
		return 0, fmt.Errorf("%s: no top-level locals block sets caddy_image, the image the Renovate comment pins: no line carries %q above it (CLAUDE.md rule 7, docs/adr/0045)", containerPath, renovateComment)
	case 1:
		return pin, nil
	default:
		return 0, fmt.Errorf("%s: caddy_image is set twice: the reader cannot tell which image the platform runs (docs/adr/0045)", containerPath)
	}
}

// renovatePinned reports whether the line above the one at offset holds nothing but the Renovate comment, a #
// comment the scan read as one (so not inside a /* */ comment or a string), and whether offset is the first
// thing on its own line.
func renovatePinned(src string, comments []hclToken, offset int) bool {
	lineStart := strings.LastIndexByte(src[:offset], '\n') + 1
	if strings.TrimSpace(src[lineStart:offset]) != "" || lineStart == 0 {
		return false
	}

	prevStart := strings.LastIndexByte(src[:lineStart-1], '\n') + 1
	prev := src[prevStart : lineStart-1]
	at := prevStart + len(prev) - len(strings.TrimLeft(prev, " \t"))

	for _, c := range comments {
		if c.start == at {
			return strings.TrimRight(c.text, " \t\r") == renovateComment
		}
	}

	return false
}

// lineOf returns the 1-based line of the offset in src.
func lineOf(src string, offset int) int {
	return strings.Count(src[:offset], "\n") + 1
}

// isImageDigest reports whether s is exactly the 64 lower-case hex characters of a sha256 digest, and nothing
// after them: the digest a pinned image reference ends with (docs/adr/0045).
func isImageDigest(s string) bool {
	if len(s) != 64 {
		return false
	}

	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' || s[i] >= 'a' && s[i] <= 'f' {
			continue
		}

		return false
	}

	return true
}

// hclList returns the elements of the attribute's list, from src, as their text:
// command = ["/bin/sh", "-c"]. The list is read from the same tokens as the
// attribute (hclTokens), so a multi-line list, a #, // or /* */ comment between
// its elements and white space around the brackets are read too, while a comment
// marker inside a quoted string ("a // b") stays part of that string. An element's
// text runs from its first token to its last, so a comment inside one element
// stays in its text and fails checkElements. Every element must be a quoted
// string or a file() call, so a list this reader does not understand fails the
// test rather than running something else. The closing ] ends the attribute's
// value only when nothing is left in the body or the next attribute or block
// starts there: a list an expression follows (["a"] == [] ? ["x"] : ["y"]) is
// refused, since the platform runs the expression's value rather than the list.
func hclList(src, attribute string) ([]string, error) {
	tokens, err := hclTokens(src)
	if err != nil {
		return nil, err
	}

	body, err := containerBody(tokens)
	if err != nil {
		return nil, err
	}

	j, err := attrValue(body, attribute)
	if err != nil {
		return nil, err
	}

	if j >= len(body) || body[j].kind != tokPunct || body[j].text != "[" {
		return nil, fmt.Errorf("%s is not a list", attribute)
	}

	var elements []string

	first, last := -1, -1
	flush := func() {
		if first >= 0 {
			end := body[last].start + len(body[last].text)
			elements = append(elements, src[body[first].start:end])
		}
		first, last = -1, -1
	}

	for k, depth := j+1, 0; k < len(body); k++ {
		tok := body[k]

		if tok.kind == tokPunct {
			switch {
			case tok.text == "(" || tok.text == "[" || tok.text == "{":
				depth++
			case tok.text == ")" || tok.text == "}":
				depth--
			case tok.text == "]" && depth > 0:
				depth--
			case tok.text == "]":
				flush()

				if !valueEnds(body, k+1) {
					return nil, fmt.Errorf("%s: an expression follows the list: the platform runs the expression's value, not the list (docs/adr/0045)", attribute)
				}

				return elements, checkElements(attribute, elements)
			case tok.text == "," && depth == 0:
				flush()

				continue
			}
		}

		if first < 0 {
			first = k
		}
		last = k
	}

	return nil, fmt.Errorf("%s's list is not closed by ]", attribute)
}

// checkElements holds every element of a list to a quoted string or a file()
// call, which is what the reader hands the container.
func checkElements(attribute string, elements []string) error {
	for _, element := range elements {
		if strings.HasPrefix(element, `"`) {
			if _, err := strconv.Unquote(element); err != nil {
				return fmt.Errorf("%s's element %s: %w", attribute, element, err)
			}

			continue
		}
		if _, ok := hclFileCall(element); !ok {
			return fmt.Errorf("%s's element %s is neither a quoted string nor a file() call: this reader runs only what it understands (docs/adr/0045)", attribute, element)
		}
	}

	return nil
}

// attrValue returns the index in body of the first token of the attribute's
// value, just past `attribute =`, or len(body) when nothing follows it. body is
// the container block's tokens (containerBody), the block the container's start
// command lives in: the same attribute in another block, or in the namespace
// beside it, is no start command. A name inside a comment (a #, // or /* */ one)
// or a quoted string is no token, so `# args = [...]` and a description holding
// `args = [` are skipped. Two settings of the attribute in that block are
// refused: the reader cannot tell which one the platform reads, so it runs
// neither. Only the block's own attributes count, not those of a block or object
// nested in it.
func attrValue(body []hclToken, attribute string) (int, error) {
	values := attrValues(body, attribute)

	switch len(values) {
	case 0:
		return 0, fmt.Errorf("no %s attribute in the site container: the reader runs what the platform runs (docs/adr/0045)", attribute)
	case 1:
		return values[0], nil
	default:
		return 0, fmt.Errorf("%s is set twice in the container: the reader cannot tell which one the platform reads, so it runs neither (docs/adr/0045)", attribute)
	}
}

// attrValues returns, for every setting of the attribute among body's own attributes (not those of a block or
// object nested in it), the index of the first token of its value, just past `attribute =`.
func attrValues(body []hclToken, attribute string) []int {
	var values []int

	for j, depth := 0, 0; j < len(body); j++ {
		tok := body[j]

		switch {
		case tok.kind == tokPunct && (tok.text == "{" || tok.text == "[" || tok.text == "("):
			depth++
		case tok.kind == tokPunct && (tok.text == "}" || tok.text == "]" || tok.text == ")"):
			depth--
		case depth == 0 && tok.kind == tokIdent && tok.text == attribute && isAssignment(body, j+1):
			values = append(values, j+2)
		}
	}

	return values
}

// isAssignment reports whether body[k] is an attribute's = (and not the first = of an ==).
func isAssignment(body []hclToken, k int) bool {
	return k < len(body) && body[k].kind == tokPunct && body[k].text == "=" &&
		(k+1 >= len(body) || body[k+1].kind != tokPunct || body[k+1].text != "=")
}

// valueEnds reports whether an attribute's value may end just before body[k]: nothing is left in the body, or
// the next attribute (a name and its =, not ==) or block (a name and its { or string label) starts there.
// Anything else continues the expression, whose value is what the platform reads.
func valueEnds(body []hclToken, k int) bool {
	if k >= len(body) {
		return true
	}

	if body[k].kind != tokIdent || k+1 >= len(body) {
		return false
	}

	next := body[k+1]

	return next.kind == tokString || next.kind == tokPunct && next.text == "{" || isAssignment(body, k+1)
}

// blockBody returns the tokens between the { at tokens[open] and its closing }, and the index of that }, or
// the rest of the tokens and len(tokens) when none closes it.
func blockBody(tokens []hclToken, open int) ([]hclToken, int) {
	for j, inner := open+1, 0; j < len(tokens); j++ {
		if tokens[j].kind != tokPunct {
			continue
		}

		switch tokens[j].text {
		case "{":
			inner++
		case "}":
			if inner == 0 {
				return tokens[open+1 : j], j
			}
			inner--
		}
	}

	return tokens[open+1:], len(tokens)
}

// The kinds of token hclTokens reads.
const (
	tokIdent  = iota // a name, or a number
	tokString        // a quoted string, its text with the quotes
	tokPunct         // any other one character
)

// hclToken is one token of a .tf file and its offset in it.
type hclToken struct {
	kind  int
	text  string
	start int
}

// hclTokens reads src into its tokens, skipping white space, # and // comments
// up to the end of their line, /* */ comments across lines and the inside of
// quoted strings, so that nothing a comment or a string holds is read as code. An
// unclosed /* */ comment or string is refused, and so is every string this
// reader cannot end for certain (hclStringEnd): a template, which can nest a
// string in the string, and a raw newline, which ends no HCL string. A << outside
// a string and a comment opens a heredoc, whose body HCL reads as one string and
// this reader would read as code, so it is refused too. The scan
// covers the whole file, so no string anywhere can shift what the reader takes
// for code.
func hclTokens(src string) ([]hclToken, error) {
	tokens, _, err := hclScan(src)

	return tokens, err
}

// hclScan reads src as hclTokens does, and also returns its # and // comments, each a token from its marker
// to the end of its line, so a reader can tell a live comment line from one a /* */ comment holds.
func hclScan(src string) ([]hclToken, []hclToken, error) {
	var tokens, comments []hclToken

	for i := 0; i < len(src); {
		c := src[i]

		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			i++
		case c == '#' || c == '/' && i+1 < len(src) && src[i+1] == '/':
			start := i
			for i < len(src) && src[i] != '\n' {
				i++
			}
			comments = append(comments, hclToken{kind: tokPunct, text: src[start:i], start: start})
		case c == '<' && i+1 < len(src) && src[i+1] == '<':
			return nil, nil, fmt.Errorf("%s: line %d opens a heredoc (<<), whose body HCL reads as one string and this reader would read as code: write a quoted string (docs/adr/0045)", containerPath, lineOf(src, i))
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				return nil, nil, fmt.Errorf("%s: a /* comment is not closed by */", containerPath)
			}
			i += 2 + end + 2
		case c == '"':
			end, err := hclStringEnd(src, i, tokens)
			if err != nil {
				return nil, nil, err
			}
			tokens = append(tokens, hclToken{kind: tokString, text: src[i:end], start: i})
			i = end
		case isIdent(c):
			j := i
			for j < len(src) && (isIdent(src[j]) || src[j] == '-') {
				j++
			}
			tokens = append(tokens, hclToken{kind: tokIdent, text: src[i:j], start: i})
			i = j
		default:
			tokens = append(tokens, hclToken{kind: tokPunct, text: src[i : i+1], start: i})
			i++
		}
	}

	return tokens, comments, nil
}

// caddyfileArg is the one template string the reader accepts: file()'s argument,
// which names the Caddyfile the container writes (docs/adr/0045).
const caddyfileArg = `"${path.module}/Caddyfile"`

// hclStringEnd returns the offset just past the quoted string that opens at
// src[start], whose tokens before it are before. It fails closed on what would
// let a reader that ends a string at its first unescaped quote misread the file:
//
//   - a ${ (interpolation) or %{ (directive) template, which can hold a nested
//     string ("${ "(" }") whose quotes and brackets that reader would take for
//     code; the escapes $${ and %%{ are a literal ${ and %{, no template. The one
//     exception is file("${path.module}/Caddyfile"), the argument the container
//     reads the front's Caddyfile from, matched as exactly those bytes after the
//     tokens file and (;
//   - a raw newline before the closing quote, which ends no HCL string: tofu
//     refuses it, and a reader that ran over it would read the rest of the file
//     as one string.
//
// A run of more than two $ or % before a { is refused as a template too, rather
// than guessed at.
func hclStringEnd(src string, start int, before []hclToken) (int, error) {
	line := strings.Count(src[:start], "\n") + 1

	for j := start + 1; j < len(src); j++ {
		switch c := src[j]; c {
		case '"':
			return j + 1, nil
		case '\n':
			return 0, fmt.Errorf("%s: the string on line %d holds a raw newline, which ends no HCL string: tofu refuses it, and this reader would read the rest of the file as one string (docs/adr/0045)", containerPath, line)
		case '\\':
			if j+1 < len(src) && src[j+1] == '\n' {
				return 0, fmt.Errorf("%s: the string on line %d holds a raw newline after a \\, which ends no HCL string (docs/adr/0045)", containerPath, line)
			}
			j++
		case '$', '%':
			run := j
			for run < len(src) && src[run] == c {
				run++
			}

			if run < len(src) && src[run] == '{' && run-j != 2 {
				if c == '$' && isFileCall(before) && strings.HasPrefix(src[start:], caddyfileArg) {
					return start + len(caddyfileArg), nil
				}

				what := "interpolation"
				if c == '%' {
					what = "directive"
				}

				return 0, fmt.Errorf("%s: the string on line %d holds an HCL template (%s), which this reader does not run and cannot end for certain: write %c%c{ for a literal %c{; the only template string it reads is file(%s) (docs/adr/0045)", containerPath, line, what, c, c, c, caddyfileArg)
			}

			j = run - 1
		}
	}

	return 0, fmt.Errorf("%s: a quoted string is not closed", containerPath)
}

// isFileCall reports whether tokens end with `file (`, so the string that
// follows is a file() call's argument.
func isFileCall(tokens []hclToken) bool {
	n := len(tokens)

	return n >= 2 && tokens[n-2].kind == tokIdent && tokens[n-2].text == "file" &&
		tokens[n-1].kind == tokPunct && tokens[n-1].text == "("
}

// containerBody returns the tokens inside resource "scaleway_container" "site"
// { ... }, the block the start command lives in. A file without that block is an
// error: every token of the file is no container's body, and a reader that took
// them for one would run a command line the platform never runs
// (docs/adr/0045).
func containerBody(tokens []hclToken) ([]hclToken, error) {
	for i, depth := 0, 0; i < len(tokens); i++ {
		tok := tokens[i]

		if tok.kind == tokPunct && tok.text == "{" {
			depth++
		}
		if tok.kind == tokPunct && tok.text == "}" {
			depth--
		}

		if depth != 0 || i+3 >= len(tokens) || tok.kind != tokIdent || tok.text != "resource" ||
			tokens[i+1].text != `"scaleway_container"` || tokens[i+2].text != `"site"` || tokens[i+3].text != "{" {
			continue
		}

		body, _ := blockBody(tokens, i+3)

		return body, nil
	}

	return nil, fmt.Errorf("%s: no resource \"scaleway_container\" \"site\" block: that block is where the start command lives, and the rest of the file is no command line the platform runs (docs/adr/0045)", containerPath)
}

// isIdent reports whether b may be part of a name in a .tf file.
func isIdent(b byte) bool {
	return b == '_' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

// hclFileCall reports whether element is a file("path") call, and returns the
// path unquoted.
func hclFileCall(element string) (string, bool) {
	element = strings.TrimSpace(element)
	if !strings.HasPrefix(element, "file(") || !strings.HasSuffix(element, ")") {
		return "", false
	}

	inner := strings.TrimSpace(element[len("file(") : len(element)-1])

	path, err := strconv.Unquote(inner)
	if err != nil {
		return "", false
	}

	return path, true
}

// caddyImageLocal is what the site container's image must be (docs/adr/0045): the local the Renovate comment
// pins, written once and read by the container, so the one line Renovate moves is the line the platform's
// image comes from, and the reader reads that same line.
const caddyImageLocal = "local.caddy_image"

// renovateComment is the line above the image pin (CLAUDE.md rule 7), as infra/site/container.tf writes it.
const renovateComment = "# renovate: " + imagePinComment

// imageLocals returns a container.tf's top-level locals block with the one local the Renovate comment pins,
// shaped as infra/site/container.tf writes it: the comment on the line above the local it pins.
func imageLocals(pin string) string {
	return "locals {\n  " + renovateComment + "\n  caddy_image = \"" + pin + "\"\n}\n\n"
}

// containerBlock returns the site container's resource block, the block the start command lives in: its image
// is the expression image, and body is the rest of its body.
func containerBlock(image, body string) string {
	return "resource \"scaleway_container\" \"site\" {\n  image = " + image + "\n" + body + "}\n"
}

// containerTF returns a container.tf's text shaped like infra/site/container.tf: the locals block that pins
// pin, and the site container, whose image reads that local, with body as the rest of its body.
func containerTF(pin, body string) string {
	return imageLocals(pin) + containerBlock(caddyImageLocal, body)
}

// TestContainerRunFrom pins the reader on the forms container.tf may take and on
// the ones it must refuse, so that the behaviour test runs the container's
// command line rather than something it misread.
func TestContainerRunFrom(t *testing.T) {
	t.Parallel()

	const (
		// digestTail is the 64 lower-case hex characters of the pinned image's sha256 digest.
		digestTail = "003a97808e781aa9f7e75cc2f43d34d461099031faf5e20faefb9f61126836f0"
		digest     = "docker.io/library/caddy:2.11.9-alpine@sha256:" + digestTail
		// caddyAt is a shorter reference pinned by the same digest, for the cases that are about the command
		// line rather than the image.
		caddyAt = "caddy@sha256:" + digestTail
		// shellOnly is the command the cases that are about the args write: the design's shell on one line.
		shellOnly = "  command = [\"/bin/sh\", \"-c\"]\n"
		// designed is the container's body in the design's form: the shell that writes the Caddyfile and execs
		// Caddy on it (docs/adr/0045).
		designed = shellOnly + `  args    = ["printf '%s\n' \"$1\" > /tmp/Caddyfile && exec caddy", "caddy", file("${path.module}/Caddyfile")]
`
	)

	tests := []struct {
		name    string
		tf      string
		want    containerRun
		wantErr string
	}{
		{
			name: "the designed one-line lists",
			tf:   containerTF(digest, designed),
			want: containerRun{
				image: digest,
				argv: []string{
					"/bin/sh",
					"-c",
					"printf '%s\n' \"$1\" > /tmp/Caddyfile && exec caddy",
					"caddy",
					"Caddyfile bytes",
				},
			},
		},
		{
			name: "a multi-line list with a comment",
			tf: containerTF(caddyAt, `  command = [
    "/bin/sh", # the shell
    "-c",
  ]
  args = [
    "run",
    "caddy",
  ]
`),
			want: containerRun{
				image: caddyAt,
				argv:  []string{"/bin/sh", "-c", "run", "caddy"},
			},
		},
		{
			name: "a multi-line list with // and /* */ comments between its elements",
			tf: containerTF(caddyAt, `  command = ["/bin/sh", "-c"]
  args = [
    "run", // the subcommand
    "caddy", /* the program
    the shell execs */
    "a // b",
    "c /* d */",
  ]
`),
			want: containerRun{
				image: caddyAt,
				argv:  []string{"/bin/sh", "-c", "run", "caddy", "a // b", "c /* d */"},
			},
		},
		{
			name: "a commented-out list above the real one",
			tf: containerTF(caddyAt, `  # command = ["/bin/bash", "-c"]
  command = ["/bin/sh", "-c"]
  // args = ["evil"]
  args = ["run", "caddy"] # args = ["trailing"]
`),
			want: containerRun{
				image: caddyAt,
				argv:  []string{"/bin/sh", "-c", "run", "caddy"},
			},
		},
		{
			name: "a block comment holding the attribute above the real one",
			tf: containerTF(caddyAt, `  command = ["/bin/sh", "-c"]
  /* args = ["evil"] */
  args = ["run", "caddy"]
`),
			want: containerRun{
				image: caddyAt,
				argv:  []string{"/bin/sh", "-c", "run", "caddy"},
			},
		},
		{
			name: "a multi-line block comment holding the attribute above the real one",
			tf: containerTF(caddyAt, `  command = ["/bin/sh", "-c"]
  /*
    args = ["evil"]
  */
  args = ["run", "caddy"]
`),
			want: containerRun{
				image: caddyAt,
				argv:  []string{"/bin/sh", "-c", "run", "caddy"},
			},
		},
		{
			name: "the attribute named inside a quoted string above the real one",
			tf: containerTF(caddyAt, `  description = "the string that prints args = [\"evil\"] to the log"
  command     = ["/bin/sh", "-c"]
  args        = ["run", "caddy"]
`),
			want: containerRun{
				image: caddyAt,
				argv:  []string{"/bin/sh", "-c", "run", "caddy"},
			},
		},
		{
			name: "the attribute in another block before the container",
			tf: imageLocals(caddyAt) + `resource "scaleway_container_namespace" "site" {
  name = "shift2eu-site"
  args = ["evil"]
}

` + containerBlock(caddyImageLocal, `  command = ["/bin/sh", "-c"]
  args    = ["run", "caddy"]
`),
			want: containerRun{
				image: caddyAt,
				argv:  []string{"/bin/sh", "-c", "run", "caddy"},
			},
		},
		// A file with no site container block holds no start command: the reader refuses it rather than reading
		// every token of the file as one (issue #566, point 8).
		{
			name: "no site container block",
			tf: imageLocals(caddyAt) + `resource "scaleway_container_namespace" "site" {
  name    = "shift2eu-site"
  command = ["/bin/sh", "-c"]
  args    = ["run", "caddy"]
}
`,
			wantErr: `no resource "scaleway_container" "site" block`,
		},
		// A list's closing ] ends the attribute's value only when nothing follows it: an expression after it (a
		// comparison, a conditional) is the value the platform runs, and a reader that stopped at the ] would
		// run the list the expression only compares against. So what follows the ] must be the end of the body,
		// or the start of the next attribute or block.
		{
			name: "the args followed by a comparison and a conditional",
			tf: containerTF(caddyAt, shellOnly+`  args    = ["a"] == [] ? ["x"] : ["y"]
`),
			wantErr: "an expression follows the list",
		},
		{
			name: "the command followed by a comment, a comparison and a conditional",
			tf: containerTF(caddyAt, `  command = ["/bin/sh", "-c"] /* x */ == [] ? [] : []
  args    = ["run", "caddy"]
`),
			wantErr: "an expression follows the list",
		},
		{
			name: "the attribute set twice in the container",
			tf: containerTF(caddyAt, `  command = ["/bin/sh", "-c"]
  args    = ["run", "caddy"]
  args    = ["evil"]
`),
			wantErr: "twice",
		},
		{
			name: "a digest truncated below its 64 hex characters",
			tf:   containerTF("caddy@sha256:"+digestTail[:63], designed),
			// The reader refuses the digest before it looks for a command.
			wantErr: "exactly 64 lower-case hex",
		},
		{
			name:    "a digest with one character after its 64 hex characters",
			tf:      containerTF("caddy@sha256:"+digestTail+"0", designed),
			wantErr: "exactly 64 lower-case hex",
		},
		{
			name:    "a digest written in upper-case hex",
			tf:      containerTF("caddy@sha256:"+strings.ToUpper(digestTail), designed),
			wantErr: "exactly 64 lower-case hex",
		},
		{
			name: "an image without a digest",
			tf:   containerTF("docker.io/library/caddy:2.11.9", designed),
			want: containerRun{},
			// The reader refuses the tag before it looks for a command.
			wantErr: "pinned by digest",
		},
		{
			name:    "no Renovate comment",
			tf:      "locals {\n  caddy_image = \"" + caddyAt + "\"\n}\n\n" + containerBlock(caddyImageLocal, designed),
			wantErr: "no line carries",
		},
		// The container's image is the pinned local and nothing else (docs/adr/0045): a literal, or another local,
		// is a reference the Renovate comment never moves, so the reader cannot tell which image the platform runs.
		{
			name:    "an image literal in the container",
			tf:      imageLocals(caddyAt) + containerBlock(`"caddy:latest"`, designed),
			wantErr: "local.caddy_image",
		},
		{
			name:    "another local as the container's image",
			tf:      imageLocals(caddyAt) + containerBlock("local.other_image", designed),
			wantErr: "local.caddy_image",
		},
		// A pin inside a /* */ comment is no pin: the live local below carries none, so the reader refuses the file
		// rather than read the commented-out digest.
		{
			name: "a pin inside a block comment above an unpinned local",
			tf: "locals {\n  /*\n  " + renovateComment + "\n  caddy_image = \"caddy@sha256:" + digestTail + "\"\n  */\n" +
				"  caddy_image = \"caddy:latest\"\n}\n\n" + containerBlock(caddyImageLocal, designed),
			wantErr: "Renovate comment",
		},
		// A heredoc is no quoted string: HCL reads its body as one string, while this reader would read the body as
		// code (dev/infra-rules.awk refuses a << under [parse] for the same reason).
		{
			name: "a heredoc in the container holding the args and a bracket",
			tf: containerTF(caddyAt, `  description = <<EOT
  args = ["fake"]
  (
  EOT
  command = ["/bin/sh", "-c"]
  args    = ["run", "caddy"]
`),
			wantErr: "heredoc",
		},
		{
			name: "a heredoc in another block holding the container's closing brace and a fake container",
			tf: imageLocals(caddyAt) + `resource "scaleway_container_namespace" "site" {
  name        = "shift2eu-site"
  description = <<EOT
}
resource "scaleway_container" "site" {
  image   = local.caddy_image
  command = ["/bin/bash", "-c"]
  args    = ["evil"]
}
EOT
}

` + containerBlock(caddyImageLocal, `  command = ["/bin/sh", "-c"]
  args    = ["run", "caddy"]
`),
			wantErr: "heredoc",
		},
		{
			name: "an element the reader does not understand",
			tf: containerTF(caddyAt, shellOnly+`  args    = [var.script, "caddy"]
`),
			wantErr: "neither a quoted string nor a file() call",
		},
		{
			name: "an element with a template interpolation",
			tf: containerTF(caddyAt, shellOnly+`  args    = ["run", "${var.script}"]
`),
			wantErr: "an HCL template (interpolation)",
		},
		{
			name: "an element with a template directive",
			tf: containerTF(caddyAt, `  command = ["/bin/sh", "%{ if true }-c%{ endif }"]
  args    = ["run"]
`),
			wantErr: "an HCL template (directive)",
		},
		// HCL decodes the escapes $${ and %%{ to a literal ${ and %{ (the hclsyntax spec, Template Literals), so
		// the reader's own decoding would be right; it refuses them in a command or args element all the same, and
		// an escape that decodes to a $ or a % too, so an element never reaches the platform through a spelling
		// this reader reads one way and the platform another. container.tf's own args hold a raw $ and %, never an
		// escape of either, so the real file is none of these cases.
		{
			name: "an element that escapes the template syntax with a doubled dollar",
			tf: containerTF(caddyAt, shellOnly+`  args    = ["echo $${HOME}"]
`),
			wantErr: "$${",
		},
		{
			name: "an element that escapes the template syntax with a doubled percent",
			tf: containerTF(caddyAt, shellOnly+`  args    = ["echo %%{literal}"]
`),
			wantErr: "%%{",
		},
		{
			name: "an element with an escape that decodes to a dollar, \\u0024",
			tf: containerTF(caddyAt, shellOnly+`  args    = ["a\u0024b"]
`),
			wantErr: "decodes to",
		},
		{
			name: "an element with an escape that decodes to a dollar, \\U00000024",
			tf: containerTF(caddyAt, shellOnly+`  args    = ["a\U00000024b"]
`),
			wantErr: "decodes to",
		},
		{
			name: "an element with an escape that decodes to a percent, \\U00000025",
			tf: containerTF(caddyAt, shellOnly+`  args    = ["a\U00000025b"]
`),
			wantErr: "decodes to",
		},
		// A quoted string anywhere in the container block is a string this reader must read as a string: a
		// template inside one nests a string in a string ("${ "x" }"), and a reader that ends a string at the
		// first unescaped quote reads what stands inside the nested one as code, so it can return a command line
		// the platform never runs. Every such string is refused, except file()'s Caddyfile argument below.
		{
			name: "a description holding a nested template string before the args",
			tf: containerTF(caddyAt, `  description = "${ "(" }The site's Caddy front"
  command     = ["/bin/sh", "-c"]
  args        = ["run", "caddy"]
`),
			wantErr: "holds an HCL template (interpolation)",
		},
		{
			name: "a name holding a nested template expression after the args",
			tf: containerTF(caddyAt, `  command = ["/bin/sh", "-c"]
  args    = ["run", "caddy"]
  name    = "front${ "))))${ length({ args = ["evil"] }) }" }"
`),
			wantErr: "holds an HCL template (interpolation)",
		},
		{
			name: "a description whose interpolation holds only an empty string",
			tf: containerTF(caddyAt, `  description = "${"{"}"
  command     = ["/bin/sh", "-c"]
  args        = ["run", "caddy"]
`),
			wantErr: "holds an HCL template (interpolation)",
		},
		{
			name: "a name holding a plain template above the real command",
			tf: containerTF(caddyAt, `  name    = "${var.prefix}-front"
  command = ["/bin/sh", "-c"]
  args    = ["run", "caddy"]
`),
			wantErr: "holds an HCL template (interpolation)",
		},
		{
			name: "a description holding a template directive",
			tf: containerTF(caddyAt, `  description = "%{ if true }front%{ endif }"
  command     = ["/bin/sh", "-c"]
  args        = ["run", "caddy"]
`),
			wantErr: "holds an HCL template (directive)",
		},
		// A raw newline ends no quoted string in HCL: tofu refuses one, so a reader that runs over it reads the
		// rest of the file as one string.
		{
			name: "a description string that reaches a newline",
			tf: containerTF(caddyAt, `  description = "The site's Caddy
  front"
  command     = ["/bin/sh", "-c"]
  args        = ["run", "caddy"]
`),
			wantErr: "holds a raw newline",
		},
		// The escapes HCL writes for a literal template are no template, so a string of the block that is no
		// command or args element may hold them: the refusal above is about a template the reader would have to
		// run, and an element of args or command that holds one is refused on its own ground (above).
		{
			name: "a description that escapes the template syntax",
			tf: containerTF(caddyAt, `  description = "$${literal} and %%{literal} front"
  command     = ["/bin/sh", "-c"]
  args        = ["run", "caddy"]
`),
			want: containerRun{
				image: caddyAt,
				argv:  []string{"/bin/sh", "-c", "run", "caddy"},
			},
		},
		{
			name: "an element that escapes what HCL escapes",
			tf: containerTF(caddyAt, `  command = ["/bin/sh", "-c"]
  args    = ["line\n", "carriage\r", "tab\t", "quote\"", "backslash\\", "unicode\u0041", "unicode long\U00000041"]
`),
			want: containerRun{
				image: caddyAt,
				argv: []string{
					"/bin/sh", "-c",
					"line\n", "carriage\r", "tab\t", "quote\"", "backslash\\", "unicodeA", "unicode longA",
				},
			},
		},
		// HCL's string escapes are \n \r \t \" \\ \uNNNN and \UNNNNNNNN, and no others: the Go escapes
		// \x41, \a and the octal \101 are no HCL, so the platform would refuse the configuration, and a reader
		// that decoded them would run a command line the configuration does not hold.
		{
			name: "an element with an escape HCL does not have, \\x41",
			tf: containerTF(caddyAt, shellOnly+`  args    = ["a\x41"]
`),
			wantErr: "\\x41",
		},
		{
			name: "an element with an escape HCL does not have, \\a",
			tf: containerTF(caddyAt, shellOnly+`  args    = ["a\a"]
`),
			wantErr: "\\a",
		},
		{
			name: "an element with an escape HCL does not have, the octal \\101",
			tf: containerTF(caddyAt, shellOnly+`  args    = ["a\101"]
`),
			wantErr: "\\101",
		},
		{
			name: "a file() that is not the Caddyfile",
			tf: containerTF(caddyAt, shellOnly+`  args    = ["run", file("${path.module}/other")]
`),
			wantErr: "${path.module}/Caddyfile",
		},
		{
			name:    "no args attribute",
			tf:      containerTF(caddyAt, shellOnly),
			wantErr: "no args attribute",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := containerRunFrom(tc.tf, "Caddyfile bytes")
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("containerRunFrom(%q) = %+v, want an error naming %q", tc.tf, got, tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("containerRunFrom(%q) reports %q, want one naming %q", tc.tf, err, tc.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("containerRunFrom(%q): %v", tc.tf, err)
			}

			if got.image != tc.want.image {
				t.Errorf("the image is %q, want %q", got.image, tc.want.image)
			}
			if len(got.argv) != len(tc.want.argv) {
				t.Fatalf("the command line is %q, want %q", got.argv, tc.want.argv)
			}
			for i := range got.argv {
				if got.argv[i] != tc.want.argv[i] {
					t.Errorf("argv[%d] = %q, want %q", i, got.argv[i], tc.want.argv[i])
				}
			}
		})
	}
}

// TestContainerRunIsTheDesigns holds infra/site/container.tf to the design the
// behaviour test runs: an image pinned by digest, started as /bin/sh -c with the
// front's Caddyfile as an argument (docs/adr/0045).
func TestContainerRunIsTheDesigns(t *testing.T) {
	t.Parallel()

	run, err := readContainerRun()
	if err != nil {
		t.Fatalf("%v", err)
	}

	if len(run.argv) < 2 || run.argv[0] != "/bin/sh" || run.argv[1] != "-c" {
		t.Errorf("the container's command line begins %q, want /bin/sh -c: the configuration reaches the upstream image through a start command, with no image of its own (docs/adr/0045)", run.argv)
	}
	if !strings.Contains(run.image, "@sha256:") {
		t.Errorf("the image reference is %q, want one pinned by digest (docs/adr/0045)", run.image)
	}
}
