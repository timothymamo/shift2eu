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
// image and its configuration*): the image reference below the Renovate comment
// and the command and args the container runs, with the Caddyfile's bytes in
// the one Terraform's file() fills in. The behaviour test then runs exactly what
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
// HCL's template escapes $${ and %%{ decoded to a literal ${ and %{. An element that carries a ${ or %{
// template is refused: the reader runs no template, so it would pass the template itself to the container
// instead of what the platform runs (docs/adr/0045).
func hclElementValue(element string) (string, error) {
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

// imageReference returns the image reference src pins: the string on the line
// below the Renovate comment, which must name a digest, so the platform runs one
// immutable image (docs/adr/0045).
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
// command = ["/bin/sh", "-c"]. A multi-line list, a comment after an element and
// white space around the brackets are read too, and every element must be a
// quoted string or a file() call, so a list this reader does not understand
// fails the test rather than running something else.
func hclList(src, attribute string) ([]string, error) {
	start, err := attrValueStart(src, attribute)
	if err != nil {
		return nil, err
	}

	if start >= len(src) || src[start] != '[' {
		return nil, fmt.Errorf("%s is not a list", attribute)
	}

	var (
		elements []string
		element  strings.Builder
		depth    int
		quoted   bool
		escaped  bool
	)

	for i := start + 1; i < len(src); i++ {
		c := src[i]

		switch {
		case escaped:
			element.WriteByte(c)
			escaped = false
		case quoted && c == '\\':
			element.WriteByte(c)
			escaped = true
		case c == '"':
			quoted = !quoted
			element.WriteByte(c)
		case quoted:
			element.WriteByte(c)
		case c == '#':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '(' || c == '[' || c == '{':
			depth++
			element.WriteByte(c)
		case c == ')' || c == '}':
			depth--
			element.WriteByte(c)
		case c == ']' && depth > 0:
			depth--
			element.WriteByte(c)
		case c == ']':
			if text := strings.TrimSpace(element.String()); text != "" {
				elements = append(elements, text)
			}

			return elements, checkElements(attribute, elements)
		case c == ',' && depth == 0:
			if text := strings.TrimSpace(element.String()); text != "" {
				elements = append(elements, text)
			}
			element.Reset()
		default:
			element.WriteByte(c)
		}
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

// attrValueStart returns the offset in src of the attribute's value, just past
// `attribute =`.
func attrValueStart(src, attribute string) (int, error) {
	for i := 0; i < len(src); i++ {
		if i > 0 && isIdent(src[i-1]) {
			continue // the end of a longer name
		}
		if !strings.HasPrefix(src[i:], attribute) {
			continue
		}
		if i+len(attribute) < len(src) && isIdent(src[i+len(attribute)]) {
			continue // the start of a longer name
		}

		rest := src[i+len(attribute):]
		value := len(rest) - len(strings.TrimLeft(rest, " \t"))

		if value >= len(rest) || rest[value] != '=' {
			continue
		}

		value++
		value += len(rest[value:]) - len(strings.TrimLeft(rest[value:], " \t"))

		return i + len(attribute) + value, nil
	}

	return 0, fmt.Errorf("no %s attribute: the container's start command is what the platform runs (docs/adr/0045)", attribute)
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

// TestContainerRunFrom pins the reader on the forms container.tf may take and on
// the ones it must refuse, so that the behaviour test runs the container's
// command line rather than something it misread.
func TestContainerRunFrom(t *testing.T) {
	t.Parallel()

	const (
		comment = "# renovate: datasource=docker depName=caddy\n"
		// digestTail is the 64 lower-case hex characters of the pinned image's sha256 digest.
		digestTail = "003a97808e781aa9f7e75cc2f43d34d461099031faf5e20faefb9f61126836f0"
		digest     = "docker.io/library/caddy:2.11.9-alpine@sha256:" + digestTail
		// caddyAt is a shorter reference pinned by the same digest, for the cases that are about the command
		// line rather than the image.
		caddyAt   = "caddy@sha256:" + digestTail
		imageLine = "image = \"" + caddyAt + "\"\n"
	)

	tests := []struct {
		name    string
		tf      string
		want    containerRun
		wantErr string
	}{
		{
			name: "the designed one-line lists",
			tf: comment + `image = "` + digest + `"

resource "scaleway_container" "site" {
  command = ["/bin/sh", "-c"]
  args    = ["printf '%s\n' \"$1\" > /tmp/Caddyfile && exec caddy", "caddy", file("${path.module}/Caddyfile")]
}
`,
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
			tf: comment + imageLine + `
resource "scaleway_container" "site" {
  command = [
    "/bin/sh", # the shell
    "-c",
  ]
  args = [
    "run",
    "caddy",
  ]
}
`,
			want: containerRun{
				image: caddyAt,
				argv:  []string{"/bin/sh", "-c", "run", "caddy"},
			},
		},
		{
			name: "a digest truncated below its 64 hex characters",
			tf:   comment + "image = \"caddy@sha256:" + digestTail[:63] + "\"\n",
			// The reader refuses the digest before it looks for a command.
			wantErr: "exactly 64 lower-case hex",
		},
		{
			name:    "a digest with one character after its 64 hex characters",
			tf:      comment + "image = \"caddy@sha256:" + digestTail + "0\"\n",
			wantErr: "exactly 64 lower-case hex",
		},
		{
			name:    "a digest written in upper-case hex",
			tf:      comment + "image = \"caddy@sha256:" + strings.ToUpper(digestTail) + "\"\n",
			wantErr: "exactly 64 lower-case hex",
		},
		{
			name: "an image without a digest",
			tf:   comment + "image = \"docker.io/library/caddy:2.11.9\"\n",
			want: containerRun{},
			// The reader refuses the tag before it looks for a command.
			wantErr: "pinned by digest",
		},
		{
			name:    "no Renovate comment",
			tf:      imageLine,
			wantErr: "no line carries",
		},
		{
			name:    "an element the reader does not understand",
			tf:      comment + imageLine + "command = [\"/bin/sh\", \"-c\"]\nargs    = [var.script, \"caddy\"]\n",
			wantErr: "neither a quoted string nor a file() call",
		},
		{
			name:    "an element with a template interpolation",
			tf:      comment + imageLine + "command = [\"/bin/sh\", \"-c\"]\nargs    = [\"run\", \"${var.script}\"]\n",
			wantErr: "an HCL template (interpolation)",
		},
		{
			name:    "an element with a template directive",
			tf:      comment + imageLine + "command = [\"/bin/sh\", \"%{ if true }-c%{ endif }\"]\nargs    = [\"run\"]\n",
			wantErr: "an HCL template (directive)",
		},
		{
			name: "an element that escapes the template syntax",
			tf:   comment + imageLine + "command = [\"/bin/sh\", \"-c\"]\nargs    = [\"$${literal}\", \"%%{literal}\"]\n",
			want: containerRun{
				image: caddyAt,
				argv:  []string{"/bin/sh", "-c", "${literal}", "%{literal}"},
			},
		},
		{
			name:    "a file() that is not the Caddyfile",
			tf:      comment + imageLine + "command = [\"/bin/sh\", \"-c\"]\nargs    = [\"run\", file(\"${path.module}/other\")]\n",
			wantErr: "${path.module}/Caddyfile",
		},
		{
			name:    "no args attribute",
			tf:      comment + imageLine + "command = [\"/bin/sh\", \"-c\"]\n",
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

// TestContainerRunIsTheDesigns fails while infra/site/container.tf does not hold
// the pinned image and the /bin/sh -c command line the behaviour test runs: RED
// until the coder writes the container (docs/adr/0045).
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
