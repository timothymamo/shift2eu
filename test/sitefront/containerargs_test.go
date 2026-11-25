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
// HCL's own escapes decoded - \n \r \t \" \\ \uNNNN and \UNNNNNNNN, and no others - and its template escapes
// $${ and %%{ decoded to a literal ${ and %{. An element that carries a ${ or %{ template is refused: the
// reader runs no template, so it would pass the template itself to the container instead of what the platform
// runs (docs/adr/0045). An escape HCL has and Go also has as another character (\x41, \a, the octal \101) is
// refused as well: tofu would refuse the configuration, and a reader that decoded them would run a command
// line the configuration does not hold.
func hclElementValue(element string) (string, error) {
	if err := checkHCLEscapes(element); err != nil {
		return "", err
	}

	value, err := strconv.Unquote(element)
	if err != nil {
		return "", err
	}

	var out strings.Builder

	for i := 0; i < len(value); i++ {
		c := value[i]
		if c != '$' && c != '%' {
			out.WriteByte(c)

			continue
		}

		if i+2 < len(value) && value[i+1] == c && value[i+2] == '{' { // $${ and %%{ are HCL's literal ${ and %{
			out.WriteByte(c)
			out.WriteByte('{')
			i += 2

			continue
		}

		if i+1 < len(value) && value[i+1] == '{' {
			what := "interpolation"
			if c == '%' {
				what = "directive"
			}

			return "", fmt.Errorf("%c{ starts an HCL template (%s), which this reader does not run: write %c%c{ for a literal %c{", c, what, c, c, c)
		}

		out.WriteByte(c)
	}

	return out.String(), nil
}

// checkHCLEscapes refuses a backslash escape in a quoted element that HCL does not have: HCL's are \n \r \t
// \" \\ \uNNNN and \UNNNNNNNN, while strconv.Unquote also decodes Go's \x41, \a, \b, \f, \v and the octal
// \101, which tofu would refuse.
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
		case c == 'u' && hexDigits(element, i+2, 4):
			i += 5
		case c == 'U' && hexDigits(element, i+2, 8):
			i += 9
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
	lines := strings.Split(src, "\n")

	for i, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") || !strings.Contains(line, imagePinComment) {
			continue
		}

		for j, next := range lines[i+1:] {
			trimmed := strings.TrimSpace(next)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}

			_, value, ok := strings.Cut(trimmed, "=")
			if !ok {
				return "", fmt.Errorf("%s: line %d is %q, want an image assignment below the Renovate comment (CLAUDE.md rule 7)", containerPath, i+j+2, trimmed)
			}

			image, err := strconv.Unquote(strings.TrimSpace(value))
			if err != nil {
				return "", fmt.Errorf("%s: unquote the image on line %d: %w", containerPath, i+j+2, err)
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

		return "", fmt.Errorf("%s: the Renovate comment on line %d has no image assignment below it (CLAUDE.md rule 7)", containerPath, i+1)
	}

	return "", fmt.Errorf("%s: no line carries %q, the comment that pins the image (CLAUDE.md rule 7, docs/adr/0045)", containerPath, imagePinComment)
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
// test rather than running something else.
func hclList(src, attribute string) ([]string, error) {
	tokens, err := hclTokens(src)
	if err != nil {
		return nil, err
	}

	body := containerBody(tokens)

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
	value, found := -1, false

	for j, depth := 0, 0; j < len(body); j++ {
		tok := body[j]

		switch {
		case tok.kind == tokPunct && (tok.text == "{" || tok.text == "[" || tok.text == "("):
			depth++
		case tok.kind == tokPunct && (tok.text == "}" || tok.text == "]" || tok.text == ")"):
			depth--
		case depth == 0 && tok.kind == tokIdent && tok.text == attribute &&
			j+1 < len(body) && body[j+1].kind == tokPunct && body[j+1].text == "=" &&
			(j+2 >= len(body) || body[j+2].kind != tokPunct || body[j+2].text != "="):
			if found {
				return 0, fmt.Errorf("%s is set twice in the container: the reader cannot tell which one the platform reads, so it runs neither (docs/adr/0045)", attribute)
			}

			found = true
			value = j + 2
		}
	}

	if !found {
		return 0, fmt.Errorf("no %s attribute: the container's start command is what the platform runs (docs/adr/0045)", attribute)
	}

	return value, nil
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
// string in the string, and a raw newline, which ends no HCL string. The scan
// covers the whole file, so no string anywhere can shift what the reader takes
// for code.
func hclTokens(src string) ([]hclToken, error) {
	var tokens []hclToken

	for i := 0; i < len(src); {
		c := src[i]

		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			i++
		case c == '#' || c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				return nil, fmt.Errorf("%s: a /* comment is not closed by */", containerPath)
			}
			i += 2 + end + 2
		case c == '"':
			end, err := hclStringEnd(src, i, tokens)
			if err != nil {
				return nil, err
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

	return tokens, nil
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
// { ... }, the block the start command lives in, or all of them when no such
// block stands at the top level.
func containerBody(tokens []hclToken) []hclToken {
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

		for j, inner := i+4, 0; j < len(tokens); j++ {
			if tokens[j].kind != tokPunct {
				continue
			}

			switch tokens[j].text {
			case "{":
				inner++
			case "}":
				if inner == 0 {
					return tokens[i+4 : j]
				}
				inner--
			}
		}

		return tokens[i+4:]
	}

	return tokens
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
		{
			name: "an element that escapes the template syntax",
			tf: containerTF(caddyAt, shellOnly+`  args    = ["$${literal}", "%%{literal}"]
`),
			want: containerRun{
				image: caddyAt,
				argv:  []string{"/bin/sh", "-c", "${literal}", "%{literal}"},
			},
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
