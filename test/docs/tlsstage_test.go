// Pins the site runbook's recovery from a TLS stage that never got its managed certificate (issue #541).
//
// On the maintainer's first real run (2026-11-24), R4's apply created the Edge Services TLS stage before R8
// had created the www CNAME at Namecheap, and Scaleway can issue the managed Let's Encrypt certificate only
// once that CNAME resolves to the pipeline, once, at the stage's creation. Hours after R8 the stage still
// read `status: active`, `certificate_expires_at: null`, and every handshake to www failed. The only way to
// ask again is to replace the stage, so R8 must say how - a plan that writes the replacement to tls.tfplan,
// then the apply of that file, never one interactive apply that plans and asks at once - and show a
// read-only command that tells a certificate from none, with a PASS comment for the one and a FAIL comment
// for the other. The plan is reviewed before it runs, so R8 must also say what makes a maintainer stop: a
// plan that replaces or destroys anything other than the TLS stage, and an apply that failed part-way, which
// is re-run. R4, whose first apply is where the certificate is requested too early, must say that R8's
// recreate step is expected on a first setup, so a maintainer meeting the FAIL line knows it is the order and
// not a fault.
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

	// tlsStagePlanPrefix starts R8's recreate step: a plan that writes the replacement to a file, so the
	// maintainer reads what it touches before any apply runs. A replace, never a destroy the DNS stage still
	// references (Scaleway answers that with a 500).
	tlsStagePlanPrefix = "dev/tofu.sh -chdir=infra/site plan -replace=" + tlsStageResource + " -out=tls.tfplan"

	// tlsStageApplyPrefix starts R8's second step: the apply of the plan the line above wrote, and of nothing
	// else.
	tlsStageApplyPrefix = "dev/tofu.sh -chdir=infra/site apply tls.tfplan"

	// tlsStageUnreviewedApplyPrefix starts an apply that plans and applies in one step: a maintainer never
	// reads its plan, and answering yes is no review, so R8 must not hold it.
	tlsStageUnreviewedApplyPrefix = "dev/tofu.sh -chdir=infra/site apply -replace="

	// tlsStageListPrefix starts R8's read-only view of the pipeline's TLS stage.
	tlsStageListPrefix = "scw edge-services tls-stage list pipeline-id="

	// tlsStageCertificateField is the field that tells an issued certificate from none: null means the stage
	// never got one.
	tlsStageCertificateField = "certificate_expires_at"

	// tlsStageRecoveryPlanEnd is how R8's recovery from an apply that failed part-way must re-plan: a plain
	// plan that writes what is left to tls.tfplan. After a part-way apply the state's current TLS stage is
	// already the new one, so re-planning with the replace target would replace it a third time.
	tlsStageRecoveryPlanEnd = "plan -out=tls.tfplan"

	// tlsStageReplaceFlag is the plan argument R8's recovery must not carry, for the reason above.
	tlsStageReplaceFlag = "-replace"

	// tlsStagePipelineIDPrefix sets the pipeline ID R8's read-only TLS stage line reads. R8 must set it
	// itself: a fresh shell holds no $PL from the earlier steps, and the list line reads "$PL".
	tlsStagePipelineIDPrefix = "PL=\"$(dev/tofu.sh -chdir=infra/site output -raw pipeline_id)\""
)

// TestTLSStageRecreateInR8 pins R8's recreate step and its check (issue #541): a plan that writes the TLS
// stage's replacement to tls.tfplan, the apply of that file later in R8 and no interactive apply, the FAIL
// comments that tell a plan touching anything else and an apply that failed part-way from a clean run, a
// read-only line that shows the stage's certificate with a PASS and a FAIL comment on it, and - in R4 or the
// order note above R0 - that the recreate step is expected on a first setup.
func TestTLSStageRecreateInR8(t *testing.T) {
	t.Parallel()

	lines := strings.Split(readDoc(t, os.DirFS(repoRoot), siteRunbookPath), "\n")
	r8 := tlsRunbookSection(t, lines, tlsStageHeading)

	t.Run("R8 plans the TLS stage's replacement and applies that plan", func(t *testing.T) {
		t.Parallel()

		planAt, _, plans := runbookLineStartingWith(r8, tlsStagePlanPrefix)
		if !plans {
			t.Errorf("%s's R8 holds no line starting %q: Scaleway issues the managed certificate once, when the TLS stage is created, so a stage created before R8's CNAME never gets one and replacing it is the only way to ask again (issue #541). R8 was:\n%s",
				siteRunbookPath, tlsStagePlanPrefix, strings.Join(r8, "\n"))
		}

		applyAt, _, applies := runbookLineStartingWith(r8, tlsStageApplyPrefix)
		if !applies {
			t.Errorf("%s's R8 holds no line starting %q, the apply of the plan the recreate step writes: a maintainer reviews that plan before it runs, and the step is unfinished while it is only written (issue #541). R8 was:\n%s",
				siteRunbookPath, tlsStageApplyPrefix, strings.Join(r8, "\n"))
		}

		if plans && applies && applyAt < planAt {
			t.Errorf("%s's R8 applies %q at line %d, before the plan that writes it at line %d: the apply would run a file the step has not written yet (issue #541)",
				siteRunbookPath, tlsStageApplyPrefix, applyAt, planAt)
		}

		if number, line, ok := runbookLineStartingWith(r8, tlsStageUnreviewedApplyPrefix); ok {
			t.Errorf("%s's R8 applies with %q at line %d: one apply that plans and asks in the same breath is never reviewed, so the step must write its plan to tls.tfplan and apply that file (issue #541):\n\t%s",
				siteRunbookPath, tlsStageUnreviewedApplyPrefix, number, strings.TrimSpace(line))
		}
	})

	t.Run("R8 stops when the plan touches anything else", func(t *testing.T) {
		t.Parallel()

		failLines := tlsRunbookFailLines(r8)

		for _, line := range failLines {
			lower := strings.ToLower(line)
			if (strings.Contains(lower, "replaces") || strings.Contains(lower, "destroys")) && strings.Contains(lower, "other") {
				return
			}
		}

		t.Errorf("%s's R8 has no %q comment that names what the plan replaces or destroys with %q: the recreate step must replace the TLS stage and repoint the DNS stage's tls_stage_id, nothing else, so a plan that touches another resource is a stop, not an apply (issue #541). R8's FAIL comments were:\n%s",
			siteRunbookPath, "# FAIL", "other", strings.Join(failLines, "\n"))
	})

	t.Run("R8 says to re-run an apply that failed part-way", func(t *testing.T) {
		t.Parallel()

		failLines := tlsRunbookFailLines(r8)

		for _, line := range failLines {
			lower := strings.ToLower(line)
			if strings.Contains(lower, "part-way") || strings.Contains(lower, "partway") {
				return
			}
		}

		t.Errorf("%s's R8 has no %q comment that says to re-run an apply which failed part-way: the replacement creates the new stage and deletes the old one in two steps, and an apply that stopped between them leaves the plan still to finish, so re-running it is the recovery (issue #541). R8's FAIL comments were:\n%s",
			siteRunbookPath, "# FAIL", strings.Join(failLines, "\n"))
	})

	t.Run("R8 re-plans a part-way apply without -replace", func(t *testing.T) {
		t.Parallel()

		partway := tlsRunbookPartwayFailLines(r8)
		if len(partway) == 0 {
			t.Fatalf("%s's R8 has no %q comment about an apply that failed part-way, so this check pins nothing: the replacement creates the new stage and deletes the old one in two steps, and an apply that stopped between them needs the recovery this comment gives (issue #541). R8's FAIL comments were:\n%s",
				siteRunbookPath, "# FAIL", strings.Join(tlsRunbookFailLines(r8), "\n"))
		}

		for _, line := range partway {
			if !strings.Contains(line, tlsStageRecoveryPlanEnd) {
				t.Errorf("%s's R8 recovery from a part-way apply does not name %q: after the apply that stopped part-way the state's current TLS stage is already the new one, so the recovery is a plain plan that writes what is left to tls.tfplan, and \"the plan line above\" would replace the stage a third time (issue #541):\n\t%s",
					siteRunbookPath, tlsStageRecoveryPlanEnd, line)
			}

			if strings.Contains(line, tlsStageReplaceFlag) {
				t.Errorf("%s's R8 recovery from a part-way apply carries %q: the create that completed already left the new TLS stage in the state, so a plan that replaces it again would replace it a third time, and the recovery must plan without it (issue #541):\n\t%s",
					siteRunbookPath, tlsStageReplaceFlag, line)
			}
		}
	})

	t.Run("R8 sets the pipeline ID it lists", func(t *testing.T) {
		t.Parallel()

		listAt, listLine, ok := runbookLineStartingWith(r8, tlsStageListPrefix)
		if !ok {
			t.Fatalf("%s's R8 holds no line starting %q: the step that reads the stage's certificate needs it (issue #541). R8 was:\n%s",
				siteRunbookPath, tlsStageListPrefix, strings.Join(r8, "\n"))
		}

		if tlsRunbookLineSetsPipelineIDBefore(listLine, tlsStageListPrefix) {
			return
		}

		if _, _, sets := runbookLineStartingWith(r8[:listAt-1], tlsStagePipelineIDPrefix); sets {
			return
		}

		t.Errorf("%s's R8 lists the TLS stage with %q but sets no pipeline ID before it: neither that line nor any line of R8 before it starts %q, so the command reads an empty $PL in a fresh shell (issue #541):\n\t%s",
			siteRunbookPath, tlsStageListPrefix, tlsStagePipelineIDPrefix, strings.TrimSpace(listLine))
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

		failLines := tlsRunbookFailLines(r8)

		for _, line := range failLines {
			if strings.Contains(line, "null") && strings.Contains(strings.ToLower(line), "recreate") {
				return
			}
		}

		t.Errorf("%s's R8 has no %q comment that names %s: null and the recreate step, so a maintainer who sees no certificate cannot tell that %q means the stage must be replaced (issue #541). R8's FAIL comments were:\n%s",
			siteRunbookPath, "# FAIL", tlsStageCertificateField, "certificate_expires_at: null", strings.Join(failLines, "\n"))
	})

	t.Run("R4 expects R8's recreate step on a first setup", func(t *testing.T) {
		t.Parallel()

		r4 := tlsRunbookSection(t, lines, tlsStageSetupHeading)
		preamble := tlsRunbookPreamble(lines)

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

// tlsRunbookFailLines returns a section's FAIL comments as the runbook writes them, trimmed: the lines a
// maintainer reads when a check did not pass.
func tlsRunbookFailLines(section []string) []string {
	var failLines []string

	for _, line := range section {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# FAIL") {
			failLines = append(failLines, trimmed)
		}
	}

	return failLines
}

// tlsRunbookPartwayFailLines returns a section's FAIL comments about an apply that failed part-way, as the
// runbook writes them, trimmed: the comment whose recovery must re-plan without the replace target.
func tlsRunbookPartwayFailLines(section []string) []string {
	var partway []string

	for _, line := range tlsRunbookFailLines(section) {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "part-way") || strings.Contains(lower, "partway") {
			partway = append(partway, line)
		}
	}

	return partway
}

// tlsRunbookLineSetsPipelineIDBefore reports whether line holds the pipeline ID assignment ahead of the
// command prefix it feeds on the same line, the other shape R8 may take.
func tlsRunbookLineSetsPipelineIDBefore(line, prefix string) bool {
	at := strings.Index(line, prefix)
	if at < 0 {
		return false
	}

	return strings.Contains(line[:at], tlsStagePipelineIDPrefix)
}

// tlsRunbookPreamble returns the lines above the first `## ` heading: the order note that tells the
// maintainer the steps' order before R0 starts.
func tlsRunbookPreamble(lines []string) []string {
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
