// Pins issue #687's other half: the checks job's govulncheck install derives the Go it is built with from
// go.mod's go line at run time, so the workflow carries no GOTOOLCHAIN=go<version> line for Renovate to read.
// The generic customManager that issue #658 gave a matchString for such a line no longer reads one, and no other
// manager does either: the version exists once, in go.mod, and the second copy is gone with the matchString and
// the found-once test that only kept the two in step by hand.
//
// The test reads .github/renovate.json5 through the tokenizer, value parser and customManager helpers of
// aireviewrenovate_test.go, as renovatecaddy_test.go and renovateopentofu_test.go do: they are the package's one
// reader of the configuration Renovate actually parses. Every manager that reads the workflow is run over two
// documents - the workflow itself, and a GOTOOLCHAIN pin written the way issue #658 wrote it - and every match
// that names a GOTOOLCHAIN= line is reported, so neither dropping the line from the workflow nor dropping the
// matchString from the configuration alone passes.
package lint_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// auditWorkflowPath is the workflow the checks job's govulncheck install lives in: the scheduled security audit
// of ADR 0040.
const auditWorkflowPath = ".github/workflows/scheduled-security-audit.yml"

// auditToolchainPinLine is the install line issue #658 wrote into the workflow and issue #687 replaced: a Go
// version of its own, under the annotation Renovate's generic manager reads. No manager may match it, so a
// matchString for it cannot come back with a literal.
const auditToolchainPinLine = "# renovate: datasource=golang-version depName=go\n" +
	`          GOTOOLCHAIN=go1.27.4 GOBIN="${bin}" go install "golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}"` + "\n"

// TestNoRenovateManagerReadsAGoToolchainPin pins that the checks job's install of govulncheck gets the Go it is
// built with from go.mod at run time, and that Renovate reads no GOTOOLCHAIN=go<version> line anywhere (issue
// #687, CLAUDE.md rule 7): the workflow must hold no such line for a manager to match, and no manager may match
// one, in the workflow or in the pin text issue #658 wrote there.
func TestNoRenovateManagerReadsAGoToolchainPin(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)

	managers := renovateCustomManagers(t, root)

	src, err := os.ReadFile(filepath.Join(root, auditWorkflowPath))
	if err != nil {
		t.Fatalf("read %s: %v; the checks job's govulncheck install derives its toolchain there (issue #687)", auditWorkflowPath, err)
	}

	document := string(src)

	// The workflow still names GOTOOLCHAIN somewhere: a document naming none
	// would pass the match check below whatever a manager reads.
	names := 0

	for _, line := range strings.Split(document, "\n") {
		if strings.Contains(line, "GOTOOLCHAIN=") && !strings.HasPrefix(strings.TrimSpace(line), "#") {
			names++
		}
	}

	if names == 0 {
		t.Fatalf("%s holds no GOTOOLCHAIN= line: the checks job's govulncheck install must name the toolchain it builds the tool with, derived from go.mod's go line (issue #687)", auditWorkflowPath)
	}

	for _, tc := range []struct {
		name     string
		document string
	}{
		{name: "the audit workflow", document: document},
		{name: "the pin line issue #658 wrote", document: auditToolchainPinLine},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			for _, manager := range managers {
				if !renovateManagerScans(t, manager, auditWorkflowPath) {
					continue
				}

				for _, expr := range manager.matchStrings {
					re := compileRenovateRegexp(t, manager, expr)

					for _, match := range re.FindAllString(tc.document, -1) {
						if !strings.Contains(match, "GOTOOLCHAIN=") {
							continue
						}

						t.Errorf("%s: the customManager described %q matches %q: the checks job derives the Go govulncheck is built with from go.mod's go line at run time (issue #687), so a matchString for a GOTOOLCHAIN= line pins a version the workflow no longer carries, and its bumps would rewrite a text nothing else reads (CLAUDE.md rule 7)",
							renovateConfigPath, manager.description, strings.TrimSpace(match))
					}
				}
			}
		})
	}
}
