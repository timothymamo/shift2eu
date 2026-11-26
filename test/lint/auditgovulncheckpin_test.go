// Pins how Renovate finds the checks job's GOTOOLCHAIN pin (issue #658, CLAUDE.md rule 7). The scheduled
// audit builds govulncheck from its module with go.mod's Go, written on the install line as
// GOTOOLCHAIN=go<version> under a "# renovate: datasource=golang-version depName=go" comment, and exactly one
// matchString of the generic custom manager must read that line as the golang-version datasource's depName go,
// so Renovate's "Go toolchain" group moves the pin with go.mod's go line and the tool is never built with a
// Go the module does not use.
//
// The version the test expects is go.mod's own go line, read here rather than restated: a bump of the module's
// Go moves go.mod and the pin together, and a copy of it in this test would be the one that drifts.
//
// The test reads .github/renovate.json5 through the tokenizer, value parser and customManager helpers of
// aireviewrenovate_test.go, as renovatecaddy_test.go and renovateopentofu_test.go do: they are the package's
// one reader of the configuration Renovate actually parses.
package lint_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// auditWorkflowPinPath is the workflow the checks job's GOTOOLCHAIN pin lives in: the scheduled security
	// audit of ADR 0040.
	auditWorkflowPinPath = ".github/workflows/scheduled-security-audit.yml"

	// auditGovulncheckInstallModule is the module the checks job installs govulncheck from. It is a Go
	// program, so the go command that builds it decides which standard library its findings are about
	// (issue #658).
	auditGovulncheckInstallModule = "golang.org/x/vuln/cmd/govulncheck"
)

// auditToolchainInstallLine returns the line number of the workflow's install of govulncheck and the line
// itself, trimmed: the one line that runs go install of the module, which must carry go.mod's Go as
// GOTOOLCHAIN=go<version> on it.
func auditToolchainInstallLine(t *testing.T, workflow string) (int, string) {
	t.Helper()

	for i, line := range strings.Split(workflow, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}

		if strings.Contains(trimmed, "go install") && strings.Contains(trimmed, auditGovulncheckInstallModule+"@") {
			return i, trimmed
		}
	}

	t.Fatalf("%s holds no go install of %s@: the checks job builds the tool its govulncheck rows run, and Renovate must find the Go it is built with there (issue #658)",
		auditWorkflowPinPath, auditGovulncheckInstallModule)

	return 0, ""
}

// TestAuditGovulncheckToolchainPinIsFoundOnce pins that exactly one matchString of the Renovate
// configuration's generic custom manager reads the checks job's GOTOOLCHAIN install line, and that it reads it
// exactly once, as datasource golang-version, depName go, with go.mod's go line as its currentValue: a
// second match moves a text nothing pins, and no match leaves the tool's Go to be bumped by hand while
// go.mod's moves on (issue #658, CLAUDE.md rule 7).
func TestAuditGovulncheckToolchainPinIsFoundOnce(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)

	src, err := os.ReadFile(filepath.Join(root, auditWorkflowPinPath))
	if err != nil {
		t.Fatalf("read %s: %v; the checks job writes go.mod's Go as a GOTOOLCHAIN pin there, which Renovate moves with the module's go line (issue #658)", auditWorkflowPinPath, err)
	}

	document := string(src)
	at, line := auditToolchainInstallLine(t, document)

	if !strings.HasPrefix(line, "GOTOOLCHAIN=go") {
		t.Fatalf("%s: the go install of %s runs as %q, which does not start with GOTOOLCHAIN=go<version>: the install line is where the pin lives, so Renovate has nothing to read there (issue #658)",
			auditWorkflowPinPath, auditGovulncheckInstallModule, line)
	}

	want := goDirective(t, root)

	managers := renovateCustomManagers(t, root)

	generic, _ := aiReviewGenericManagerIndex(t, managers)

	type read struct {
		manager, datasource, depName, currentValue string
	}

	var reads []read

	for _, expr := range managers[generic].matchStrings {
		re := compileRenovateRegexp(t, managers[generic], expr)

		for _, loc := range re.FindAllStringSubmatchIndex(document, -1) {
			// The pin's match ends on the install line: it starts at the comment above it and reads the
			// GOTOOLCHAIN assignment, so its end is the line this test holds the manager to.
			if strings.Count(document[:loc[1]], "\n") != at {
				continue
			}

			match := make([]string, len(loc)/2)

			for i := range match {
				if loc[2*i] >= 0 {
					match[i] = document[loc[2*i]:loc[2*i+1]]
				}
			}

			reads = append(reads, read{
				manager:      managers[generic].description,
				datasource:   subexp(t, re, match, "datasource", managers[generic], expr),
				depName:      subexp(t, re, match, "depName", managers[generic], expr),
				currentValue: subexp(t, re, match, "currentValue", managers[generic], expr),
			})
		}
	}

	if len(reads) != 1 {
		t.Fatalf("%s: the matchStrings of the generic customManager described %q read %s's install line %q %d times (%v), want exactly once: the Go govulncheck is built with is a pinned version, so the matchString that reads it is what Renovate bumps with go.mod's go line, and none reading it, or two of them, leaves the pin behind (CLAUDE.md rule 7, issue #658)",
			renovateConfigPath, managers[generic].description, auditWorkflowPinPath, line, len(reads), reads)
	}

	got := reads[0]

	if got.datasource != "golang-version" {
		t.Errorf("%s: the matchString of the manager described %q reads %s's install line as datasource %q, want \"golang-version\": that datasource is what moves a Go toolchain pin with go.mod's go line (CLAUDE.md rule 7, issue #658)",
			renovateConfigPath, got.manager, auditWorkflowPinPath, got.datasource)
	}

	if got.depName != "go" {
		t.Errorf("%s: the matchString of the manager described %q reads %s's install line as depName %q, want \"go\": the pin names the Go toolchain, the same depName the \"Go toolchain\" group holds (CLAUDE.md rule 7, issue #658)",
			renovateConfigPath, got.manager, auditWorkflowPinPath, got.depName)
	}

	if got.currentValue != want {
		t.Errorf("%s: the matchString of the manager described %q reads %s's install line as currentValue %q, want go.mod's go line, %q: anything else is a version Renovate bumps in the wrong text, and govulncheck is then built with a Go the module does not use (CLAUDE.md rule 7, issue #658)",
			renovateConfigPath, got.manager, auditWorkflowPinPath, got.currentValue, want)
	}
}
