// Pins how Renovate finds the Caddy image pin in infra/site (PRD #297 milestone 4, ADR 0045; CLAUDE.md
// rule 7). The container the site's front runs is the official caddy image, pinned by tag and digest in
// infra/site/container.tf, with a "# renovate: datasource=docker depName=caddy" comment on the line above:
// one customManager of .github/renovate.json5 scans that file, and its matchString reads the comment and the
// HCL assignment below it as datasource docker, depName caddy, the tag as currentValue and the digest as
// currentDigest, so a bump moves the tag and the digest in one PR (ADR 0045, *The image and its
// configuration*).
//
// The tag and the digest this test expects are the ones the pin line itself holds, read here rather than
// restated: a Renovate PR that replaces the pin must not also have to touch a second copy of it, which would
// be the copy that drifts. What the test pins is the reading: the regexp captures exactly the file's one pin,
// names the image as the container does, and the line above it is the annotation.
//
// The test reads .github/renovate.json5 through the tokenizer and value parser of aireviewrenovate_test.go,
// and the customManager helpers that file holds, as renovateopentofu_test.go does: they are the package's one
// reader of the configuration Renovate actually parses.
package lint_test

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// caddyPinFile is the file the Caddy image is pinned in: the site's container, beside the Caddyfile it runs
// (ADR 0045, *The image and its configuration*). The pin lives there once, and the container's image reads it.
const caddyPinFile = "infra/site/container.tf"

// caddyPinAnnotation is the comment line Renovate's custom manager reads above the pin (CLAUDE.md rule 7):
// its matchString starts with these words, so a pin whose comment differs is one Renovate never finds.
const caddyPinAnnotation = "# renovate: datasource=docker depName=caddy"

// caddyPinLine captures the tag and the digest of the one line of caddyPinFile that pins the image: the
// container's image = "..." (or the locals value or variable default it reads), holding the reference
// docker.io/library/caddy:<tag>@sha256:<digest>. A line holding only a tag is no pin here, since a tag is
// whatever the registry serves that day.
var caddyPinLine = regexp.MustCompile(`(?m)^[ \t]*[A-Za-z_][A-Za-z0-9_]*[ \t]*=[ \t]*"docker\.io/library/caddy:([^"@\s]+)@(sha256:[a-f0-9]{64})"[ \t]*$`)

// TestCaddyImagePinIsFoundOnce pins that the Caddy pin in infra/site is one annotated pin, and that exactly
// one customManager of the Renovate configuration reads it as the caddy image: the docker datasource, the
// depName caddy, the tag and the digest the file holds.
func TestCaddyImagePinIsFoundOnce(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)

	src, err := os.ReadFile(filepath.Join(root, caddyPinFile))
	if err != nil {
		t.Fatalf("read %s: %v; ADR 0045 pins the container's Caddy image there, and Renovate bumps that one line (CLAUDE.md rule 7)", caddyPinFile, err)
	}

	tag, digest := caddyPinnedImage(t, src)
	caddyPinIsAnnotated(t, src)

	managers := renovateCustomManagers(t, root)

	type capture struct {
		manager, datasource, depName, currentValue, currentDigest string
	}

	var captures []capture

	var readers []string

	for _, manager := range managers {
		if !renovateManagerScans(t, manager, caddyPinFile) {
			continue
		}

		readers = append(readers, manager.description)

		for _, expr := range manager.matchStrings {
			re := compileRenovateRegexp(t, manager, expr)

			for _, match := range re.FindAllStringSubmatch(string(src), -1) {
				captures = append(captures, capture{
					manager:       manager.description,
					datasource:    subexp(t, re, match, "datasource", manager, expr),
					depName:       subexp(t, re, match, "depName", manager, expr),
					currentValue:  subexp(t, re, match, "currentValue", manager, expr),
					currentDigest: subexp(t, re, match, "currentDigest", manager, expr),
				})
			}
		}
	}

	if len(readers) == 0 {
		t.Fatalf("%s: no customManager's managerFilePatterns match %s, so Renovate never reads the Caddy pin and the container stays on its digest until someone bumps it by hand (ADR 0045, CLAUDE.md rule 7)", renovateConfigPath, caddyPinFile)
	}

	if len(captures) != 1 {
		t.Fatalf("%s: the customManager(s) reading %s (%v) match its pin %d times (%v), want exactly one: a second match moves a text nothing pins, and a manager that scans the file but reads nothing leaves the pin to move by hand (CLAUDE.md rule 7)",
			renovateConfigPath, caddyPinFile, readers, len(captures), captures)
	}

	got := captures[0]

	if got.datasource != "docker" {
		t.Errorf("%s: the customManager described %q reads %s's pin as datasource %q, want \"docker\": the image's tag and digest come from Docker Hub, and another datasource never moves a container image (CLAUDE.md rule 7)",
			renovateConfigPath, got.manager, caddyPinFile, got.datasource)
	}

	if got.depName != "caddy" {
		t.Errorf("%s: the customManager described %q reads %s's pin as depName %q, want \"caddy\": the image is the official caddy one, and another depName looks up another image's releases (CLAUDE.md rule 7)",
			renovateConfigPath, got.manager, caddyPinFile, got.depName)
	}

	if got.currentValue != tag {
		t.Errorf("%s: the customManager described %q reads %s's pin as currentValue %q, want the tag the pin line holds, %q: anything else is a version Renovate bumps in the wrong text (CLAUDE.md rule 7)",
			renovateConfigPath, got.manager, caddyPinFile, got.currentValue, tag)
	}

	if got.currentDigest != digest {
		t.Errorf("%s: the customManager described %q reads %s's pin as currentDigest %q, want the digest the pin line holds, %q: without the digest group Renovate moves the tag alone, and the container keeps running the old image (CLAUDE.md rule 7)",
			renovateConfigPath, got.manager, caddyPinFile, got.currentDigest, digest)
	}
}

// caddyPinnedImage returns the tag and the digest of the one line of src that pins the Caddy image, and fails
// when src holds no such line or more than one.
func caddyPinnedImage(t *testing.T, src []byte) (tag, digest string) {
	t.Helper()

	found := caddyPinLine.FindAllSubmatchIndex(src, -1)
	if len(found) != 1 {
		t.Fatalf("%s pins the image docker.io/library/caddy:<tag>@sha256:<digest> on %d lines, want exactly one: one pin, quoted, by tag and digest (ADR 0045, CLAUDE.md rule 7)", caddyPinFile, len(found))
	}

	return string(src[found[0][2]:found[0][3]]), string(src[found[0][4]:found[0][5]])
}

// caddyPinIsAnnotated fails when the line above each pin line of src is not the
// "# renovate: datasource=docker depName=caddy" comment: Renovate reads the annotation and the assignment
// below it as one pin, so a pin without it is one nothing moves.
func caddyPinIsAnnotated(t *testing.T, src []byte) {
	t.Helper()

	for _, found := range caddyPinLine.FindAllSubmatchIndex(src, -1) {
		line := bytes.LastIndexByte(src[:found[0]], '\n')
		if line <= 0 {
			t.Fatalf("%s: the pin on the file's first line has no line above it for the %q annotation (CLAUDE.md rule 7)", caddyPinFile, caddyPinAnnotation)
		}

		above := src[:line]
		start := bytes.LastIndexByte(above, '\n') + 1

		if previous := strings.TrimSpace(string(src[start:line])); !strings.HasPrefix(previous, caddyPinAnnotation) {
			t.Errorf("%s:%d: the line above the Caddy pin is %q, want the annotation %q: Renovate reads the comment and the assignment below it as one pin, so a pin without it is one nothing moves (CLAUDE.md rule 7)",
				caddyPinFile, bytes.Count(src[:found[0]], []byte("\n"))+1, previous, caddyPinAnnotation)
		}
	}
}
