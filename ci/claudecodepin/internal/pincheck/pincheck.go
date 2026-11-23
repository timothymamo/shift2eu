package pincheck

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// PackageName is the npm package the pin file pins: the Claude Code CLI's
// linux-x64 platform package (ADR 0033).
const PackageName = "@anthropic-ai/claude-code-linux-x64"

// registryBase is the npm registry the pinned package is published to.
const registryBase = "https://registry.npmjs.org/"

const (
	versionKey   = "version"
	integrityKey = "integrity"

	// integrityPrefix is the algorithm the pin file pins the tarball by.
	integrityPrefix = "sha512-"

	// sha512Size is the length of a SHA-512 digest in bytes.
	sha512Size = 64
)

var (
	// pinLine matches one top-level pin line of the pin file, unindented, and
	// captures its key and its value.
	pinLine = regexp.MustCompile(`^(` + versionKey + `|` + integrityKey + `):(.*)$`)

	// exactVersion is the only version shape the check accepts: no dist tag,
	// range, prerelease or path, so the registry URL names one release.
	exactVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
)

// Pin is the Claude Code CLI's pin in .github/claude-code.yaml: the exact
// version of PackageName and the npm integrity of its tarball.
type Pin struct {
	Version   string
	Integrity string
}

// FromPinFile reads the pin from the one pin file, .github/claude-code.yaml
// (ADR 0040's milestone-2 amendment, issue #277): exactly one top-level
// version: line and exactly one integrity: line. The version must be
// MAJOR.MINOR.PATCH and the integrity sha512- and the base64 of a SHA-512
// digest. Each value must be a plain, unquoted YAML scalar with no trailing
// comment: a quoted value or a trailing # comment fails closed, as a value
// that is not an exact version or not base64. A missing, repeated or
// malformed line is an error. The # renovate: comment above the version line
// is Renovate's and test/lint's to hold; FromPinFile reads the values alone.
func FromPinFile(r io.Reader) (Pin, error) {
	values := map[string][]string{}

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		m := pinLine.FindStringSubmatch(scanner.Text())
		if m == nil {
			continue
		}
		values[m[1]] = append(values[m[1]], strings.TrimSpace(m[2]))
	}
	if err := scanner.Err(); err != nil {
		return Pin{}, fmt.Errorf("read the pin file: %w", err)
	}

	version, err := onlyValue(values, versionKey)
	if err != nil {
		return Pin{}, err
	}
	if !exactVersion.MatchString(version) {
		return Pin{}, fmt.Errorf("%s is %q, want an exact version MAJOR.MINOR.PATCH", versionKey, version)
	}

	integrity, err := onlyValue(values, integrityKey)
	if err != nil {
		return Pin{}, err
	}
	if err := checkIntegrity(integrity); err != nil {
		return Pin{}, fmt.Errorf("%s is %q: %w", integrityKey, integrity, err)
	}

	return Pin{Version: version, Integrity: integrity}, nil
}

// onlyValue returns the one value of key, or an error when the pin file holds
// none or more than one line of it.
func onlyValue(values map[string][]string, key string) (string, error) {
	switch got := values[key]; len(got) {
	case 1:
		return got[0], nil
	case 0:
		return "", fmt.Errorf("the pin file holds no %s: line", key)
	default:
		return "", fmt.Errorf("the pin file holds %d %s: lines, want exactly one", len(got), key)
	}
}

// checkIntegrity refuses an integrity that is not sha512- and the standard
// base64 of a SHA-512 digest.
func checkIntegrity(integrity string) error {
	digest, ok := strings.CutPrefix(integrity, integrityPrefix)
	if !ok {
		return errors.New("want an integrity of the form sha512-<base64>")
	}
	raw, err := base64.StdEncoding.DecodeString(digest)
	if err != nil {
		return errors.New("the digest after sha512- is not base64")
	}
	if len(raw) != sha512Size {
		return fmt.Errorf("the digest after sha512- is %d bytes, want %d", len(raw), sha512Size)
	}
	return nil
}

// RegistryURL returns the npm registry's document of exactly the pinned
// version of PackageName.
func (p Pin) RegistryURL() string {
	return registryBase + PackageName + "/" + p.Version
}

// registryDoc is the part of a registry version document the check reads.
type registryDoc struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Dist    struct {
		Integrity string `json:"integrity"`
	} `json:"dist"`
}

// Verify reads the registry document doc and fails unless it is the document
// of PackageName at p.Version and publishes a dist.integrity equal to
// p.Integrity. A stale integrity's error names the pinned and the published
// integrity and the command that prints the published one. Text of the
// document reaches an error quoted (%q) only.
func Verify(p Pin, doc io.Reader) error {
	var d registryDoc
	if err := json.NewDecoder(doc).Decode(&d); err != nil {
		return errors.New("the registry's answer is not a JSON document")
	}

	if d.Name != PackageName {
		return fmt.Errorf("the registry's document is of package %q, want %s", d.Name, PackageName)
	}
	if d.Version != p.Version {
		return fmt.Errorf("the registry's document is of version %q, want %s", d.Version, p.Version)
	}

	published := d.Dist.Integrity
	if published == "" {
		return fmt.Errorf("the registry publishes no dist.integrity for %s@%s", PackageName, p.Version)
	}
	if err := checkIntegrity(published); err != nil {
		return fmt.Errorf("the registry publishes dist.integrity %q for %s@%s: %w", published, PackageName, p.Version, err)
	}

	if published != p.Integrity {
		return fmt.Errorf(
			"the pin file's %s: pins %s for %s@%s, but the registry publishes %s: replace it with the output of `npm view %s@%s dist.integrity`",
			integrityKey, p.Integrity, PackageName, p.Version, published, PackageName, p.Version)
	}

	return nil
}
