// Pins the site runbook's recovery from a TLS stage that never got its managed certificate (issue #541).
//
// On the maintainer's first real run (2026-11-24), R4's apply created the Edge Services TLS stage before R8
// had created the www CNAME at Namecheap, and Scaleway can issue the managed Let's Encrypt certificate only
// once that CNAME resolves to the pipeline, once, at the stage's creation. Hours after R8 the stage still
// read `status: active`, `certificate_expires_at: null`, and every handshake to www failed. The only way to
// ask again is to replace the stage, so R8 must say how - `dev/tofu.sh -chdir=infra/site apply
// -replace=scaleway_edge_services_tls_stage.site` - and show a read-only command that tells a certificate
// from none, with a PASS comment for the one and a FAIL comment for the other. R4, whose first apply is
// where the certificate is requested too early, must say that R8's recreate step is expected on a first
// setup, so a maintainer meeting the FAIL line knows it is the order and not a fault.
//
// The check reads docs/site-runbook.md's lines as the repository holds them: it needs neither the scw CLI nor
// OpenTofu and contacts nothing. Every step is judged inside its own section, so a line that belongs to
// another step cannot satisfy it.
package docs_test

import (
	"os"
	"strings"
	"testing"
)

const (
	// tlsStageHeading is R8's heading: the step whose CNAME the certificate waits for.
	tlsStageHeading = "## R8. Namecheap"

	// tlsStageSetupHeading is R4's heading: the apply that creates the TLS stage and requests the certificate.
	tlsStageSetupHeading = "## R4. Create the hosting"

	// tlsStageResource is the resource R8 replaces to ask Scaleway for a new managed certificate.
	tlsStageResource = "scaleway_edge_services_tls_stage.site"

	// tlsStageRecreatePrefix starts R8's recreate step: a replace, never a destroy the DNS stage still
	// references (Scaleway answers that with a 500).
	tlsStageRecreatePrefix = "dev/tofu.sh -chdir=infra/site apply -replace=" + tlsStageResource

	// tlsStageListPrefix starts R8's read-only view of the pipeline's TLS stage.
	tlsStageListPrefix = "scw edge-services tls-stage list pipeline-id="

	// tlsStageCertificateField is the field that tells an issued certificate from none: null means the stage
	// never got one.
	tlsStageCertificateField = "certificate_expires_at"
)

// TestTLSStageRecreateInR8 pins R8's recreate step and its check (issue #541): a line that replaces the TLS
// stage, a read-only line that shows its certificate, a PASS and a FAIL comment on that check, and - in R4 or
// the order note above R0 - that the recreate step is expected on a first setup.
func TestTLSStageRecreateInR8(t *testing.T) {
	t.Parallel()

	lines := strings.Split(readDoc(t, os.DirFS(repoRoot), siteRunbookPath), "\n")
	r8 := tlsRunbookSection(t, lines, tlsStageHeading)

	t.Run("R8 replaces the TLS stage", func(t *testing.T) {
		t.Parallel()

		if _, _, ok := runbookLineStartingWith(r8, tlsStageRecreatePrefix); !ok {
			t.Errorf("%s's R8 holds no line starting %q: Scaleway issues the managed certificate once, when the TLS stage is created, so a stage created before R8's CNAME never gets one and replacing it is the only way to ask again (issue #541). R8 was:\n%s",
				siteRunbookPath, tlsStageRecreatePrefix, strings.Join(r8, "\n"))
		}
	})

	t.Run("R8 shows the TLS stage's certificate", func(t *testing.T) {
		t.Parallel()

		_, line, ok := runbookLineStartingWith(r8, tlsStageListPrefix)
		if !ok {
			t.Fatalf("%s's R8 holds no line starting %q: the step that recreates the stage needs a read-only way to see whether a certificate came (issue #541). R8 was:\n%s",
				siteRunbookPath, tlsStageListPrefix, strings.Join(r8, "\n"))
		}

		if !strings.Contains(line, tlsStageCertificateField) {
			t.Errorf("%s's R8 lists the TLS stage without %s, so nothing tells an issued certificate from none (issue #541):\n\t%s",
				siteRunbookPath, tlsStageCertificateField, line)
		}

		if !strings.Contains(line, "# PASS:") {
			t.Errorf("%s's R8 lists the TLS stage with no %q comment, so a maintainer cannot say whether the call passed (issue #541):\n\t%s",
				siteRunbookPath, "# PASS:", line)
		}
	})

	t.Run("R8 tells a certificate that never came apart", func(t *testing.T) {
		t.Parallel()

		var failLines []string

		for _, line := range r8 {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "# FAIL") {
				continue
			}

			failLines = append(failLines, trimmed)

			if strings.Contains(trimmed, "null") && strings.Contains(strings.ToLower(trimmed), "recreate") {
				return
			}
		}

		t.Errorf("%s's R8 has no %q comment that names %s: null and the recreate step, so a maintainer who sees no certificate cannot tell that %q means the stage must be replaced (issue #541). R8's FAIL comments were:\n%s",
			siteRunbookPath, "# FAIL", tlsStageCertificateField, "certificate_expires_at: null", strings.Join(failLines, "\n"))
	})

	t.Run("R4 expects R8's recreate step on a first setup", func(t *testing.T) {
		t.Parallel()

		r4 := tlsRunbookSection(t, lines, tlsStageSetupHeading)
		preamble := tlsRunbookPreamble(t, lines)

		for _, section := range [][]string{r4, preamble} {
			if tlsRunbookSaysRecreateAfterR8(section) {
				return
			}
		}

		t.Errorf("%s's R4 (and the order note above R0) never names R8 together with \"recreate\": R4's apply creates the TLS stage before R8's CNAME exists, so the certificate cannot be issued then and R8's recreate step is expected on a first setup, not a fault (issue #541)",
			siteRunbookPath)
	})
}

// TestTLSRunbookSection pins the section helper the checks above read, so a line from another step cannot
// satisfy them: the section runs from its heading through the line before the next `## ` heading.
func TestTLSRunbookSection(t *testing.T) {
	t.Parallel()

	lines := []string{
		"# The site runbook",
		"An order note.",
		"## R4. Create the hosting",
		"dev/tofu.sh -chdir=infra/site apply site.tfplan",
		"### A subsection of R4",
		"a sub-line",
		"## R8. Namecheap",
		"dig +short CNAME www.example.com",
		"# FAIL unable to load certificate",
	}

	got := tlsRunbookSection(t, lines, "## R8. Namecheap")

	for _, line := range got {
		if line == "dev/tofu.sh -chdir=infra/site apply site.tfplan" || line == "## R4. Create the hosting" {
			t.Errorf("tlsRunbookSection() returned %q, a line of R4: a section ends at the next ## heading", line)
		}
	}

	if len(got) == 0 || got[0] != "## R8. Namecheap" {
		t.Errorf("tlsRunbookSection() = %q, want it to start at its own heading", got)
	}

	if !strings.Contains(strings.Join(got, "\n"), "# FAIL unable to load certificate") {
		t.Errorf("tlsRunbookSection() = %q, want every line of R8 through the line before the next heading", got)
	}
}

// tlsRunbookSection returns the lines of the section that heading opens: from the heading line through the
// line before the next `## ` heading, a subsection's `### ` heading not ending it. heading is the line as the
// runbook writes it; a missing one fails the test, since the step the check pins would be gone.
func tlsRunbookSection(t *testing.T, lines []string, heading string) []string {
	t.Helper()

	start := -1

	for i, line := range lines {
		if strings.TrimSpace(line) == heading {
			start = i

			break
		}
	}

	if start < 0 {
		t.Fatalf("%s holds no %q heading: the step the check pins is gone", siteRunbookPath, heading)
	}

	end := len(lines)

	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			end = i

			break
		}
	}

	return lines[start:end]
}

// tlsRunbookPreamble returns the lines above the first `## ` heading: the order note that tells the
// maintainer the steps' order before R0 starts.
func tlsRunbookPreamble(t *testing.T, lines []string) []string {
	t.Helper()

	for i, line := range lines {
		if strings.HasPrefix(line, "## ") {
			return lines[:i]
		}
	}

	return lines
}

// tlsRunbookSaysRecreateAfterR8 reports whether one line of section names R8 and says a resource is
// recreated there, the note that makes R8's recreate step expected on a first setup.
func tlsRunbookSaysRecreateAfterR8(section []string) bool {
	for _, line := range section {
		if strings.Contains(line, "R8") && strings.Contains(strings.ToLower(line), "recreate") {
			return true
		}
	}

	return false
}
