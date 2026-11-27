package architecture_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// gitleaksIgnore is the audit's ignore file at the repository root: the history
// hits the maintainer read and closed as no secret, which every run's
// full-history scan would raise again, since history cannot change (issue #658).
const gitleaksIgnore = ".gitleaksignore"

// gitleaksFingerprint matches one full gitleaks fingerprint,
// <40-hex commit>:<path>:<rule>:<line>, and nothing broader: a path, a rule or a
// commit on its own is an allow list that hides every hit gitleaks finds there,
// not the one line the maintainer read (issue #658).
var gitleaksFingerprint = regexp.MustCompile(`^[0-9a-f]{40}:[^:]+:[a-z0-9-]+:[0-9]+$`)

// gitleaksFalseHistory are the 11 fingerprints issue #658 adds, as its first two
// comments list them: four from the retired HTTP server's test placeholders and
// one line of depguard prose, and seven from the AI reviewer's and the audit
// publish's own scanner tests and one synthetic Azure fixture. Each is a test
// value or a fixture, none a secret; the test compares the sets, so the order the
// file writes them in is free.
var gitleaksFalseHistory = []string{
	"0dcb9c9ca3d96ea3ead65238e9adc2025929b7c6:cmd/eusov-server/token_test.go:generic-api-key:21",
	"0dcb9c9ca3d96ea3ead65238e9adc2025929b7c6:cmd/eusov-server/token_test.go:generic-api-key:24",
	"0dcb9c9ca3d96ea3ead65238e9adc2025929b7c6:test/e2e/server_test.go:generic-api-key:44",
	"0fa2a56a949e77a7722cc32ea672f8db02956976:ci/aireview/internal/review/scan_test.go:generic-api-key:82",
	"2476f0909507c09e40a1340bddf0db0cdca91b27:test/e2e/testdata/inventory-guide/azure/graph-1.json:generic-api-key:216",
	"301c69819fe553935d97139292997909137ab795:ci/auditpublish/internal/publish/scan_test.go:generic-api-key:449",
	"301c69819fe553935d97139292997909137ab795:ci/auditpublish/internal/publish/scan_test.go:generic-api-key:454",
	"301c69819fe553935d97139292997909137ab795:ci/auditpublish/internal/publish/scan_test.go:private-key:70",
	"301c69819fe553935d97139292997909137ab795:ci/auditpublish/internal/publish/scan_test.go:private-key:73",
	"41243e26fa8c24117adaa2e81e20b29b3fccb377:.golangci.yml:generic-api-key:810",
	"4a4473b1d2c729fd9440e2c2a91765ab37859317:ci/aireview/internal/review/scan_test.go:generic-api-key:82",
}

// TestGitleaksIgnoreHoldsOnlyTheAuditsFalseHistoryHits holds the repository's
// .gitleaksignore to what the audit needs and nothing broader: every entry is
// one full fingerprint, each is explained by a # comment directly above it, and
// the set is exactly the 11 fingerprints issue #658's first two comments name.
// A broader entry - a path or a rule alone - would silence a real leak on that
// path, and an entry nobody read would hide one the maintainer has not closed
// as a test value, so adding a line takes the same review (issue #658, ADR 0040).
func TestGitleaksIgnoreHoldsOnlyTheAuditsFalseHistoryHits(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join(repositoryRoot(t), gitleaksIgnore))
	if err != nil {
		t.Fatalf("read %s: %v: want the audit's ignore file at the repository root, holding the 11 fingerprints of the history hits the maintainer read as test values and fixtures, so the full-history scan stops raising them on every run (issue #658)", gitleaksIgnore, err)
	}

	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")

	var got []string

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if !gitleaksFingerprint.MatchString(trimmed) {
			t.Errorf("%s line %d holds %q, want one full fingerprint <40-hex commit>:<path>:<rule>:<line>: a shorter entry, or a path, a rule or a commit alone, hides every hit gitleaks finds there rather than the one line the maintainer read (issue #658)",
				gitleaksIgnore, i+1, trimmed)

			continue
		}

		if i == 0 || !strings.HasPrefix(strings.TrimSpace(lines[i-1]), "#") {
			t.Errorf("%s line %d holds the fingerprint %q with no # comment directly above it: each entry says why it is no secret, so a reader can weigh it (issue #658, ADR 0040)",
				gitleaksIgnore, i+1, trimmed)
		}

		got = append(got, trimmed)
	}

	if diff := cmp.Diff(sortedCopy(gitleaksFalseHistory), sortedCopy(got)); diff != "" {
		t.Errorf("%s's fingerprints differ from the ones issue #658's first two comments list (-want +got):\n%s", gitleaksIgnore, diff)
	}
}
