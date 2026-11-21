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

// PackageName is the npm package the workflow pins: the Claude Code CLI's
// linux-x64 platform package (ADR 0033).
const PackageName = "@anthropic-ai/claude-code-linux-x64"

// registryBase is the npm registry the pinned package is published to.
const registryBase = "https://registry.npmjs.org/"

const (
	versionKey   = "CLAUDE_CODE_VERSION"
	integrityKey = "CLAUDE_CODE_INTEGRITY"

	// integrityPrefix is the algorithm the workflow pins the tarball by.
	integrityPrefix = "sha512-"

	// sha512Size is the length of a SHA-512 digest in bytes.
	sha512Size = 64
)

var (
	// pinLine matches one pin line of an env: block, indented or not, and
	// captures its key and its value.
	pinLine = regexp.MustCompile(`^\s*(` + versionKey + `|` + integrityKey + `):(.*)$`)

	// exactVersion is the only version shape the check accepts: no dist tag,
	// range, prerelease or path, so the registry URL names one release.
	exactVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
)

// Pin is the Claude Code CLI's pin in the AI review workflow: the exact
// version of PackageName and the npm integrity of its tarball.
type Pin struct {
	Version   string
	Integrity string
}

// FromWorkflow reads the pin from the AI review workflow: exactly one
// CLAUDE_CODE_VERSION: line and exactly one CLAUDE_CODE_INTEGRITY: line. The
// version must be MAJOR.MINOR.PATCH and the integrity sha512- and the base64
// of a SHA-512 digest. A missing, repeated or malformed line is an error.
func FromWorkflow(r io.Reader) (Pin, error) {
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
		return Pin{}, fmt.Errorf("read the workflow: %w", err)
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

// onlyValue returns the one value of key, or an error when the workflow holds
// none or more than one line of it.
func onlyValue(values map[string][]string, key string) (string, error) {
	switch got := values[key]; len(got) {
	case 1:
		return got[0], nil
	case 0:
		return "", fmt.Errorf("the workflow holds no %s: line", key)
	default:
		return "", fmt.Errorf("the workflow holds %d %s: lines, want exactly one", len(got), key)
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
			"%s pins %s for %s@%s, but the registry publishes %s: replace it with the output of `npm view %s@%s dist.integrity`",
			integrityKey, p.Integrity, PackageName, p.Version, published, PackageName, p.Version)
	}

	return nil
}
