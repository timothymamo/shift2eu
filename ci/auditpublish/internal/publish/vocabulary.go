package publish

// The core's constants (ADR 0040, "The publish program's core").
const (
	// SchemaVersion is the one result schema the core reads.
	SchemaVersion = 1

	// MaxResultBytes is the largest result the core reads: 4 MiB.
	MaxResultBytes = 4 << 20

	// FindingLimit is the most findings a result may hold. A result over it
	// is refused whole, never truncated.
	FindingLimit = 100

	// ModelName is the model the audit runs, as the report's Scope names it:
	// the core's constant, never the result's.
	ModelName = "claude-fable-5-2"

	// ModelEffort is the model's effort, as the report's Scope names it.
	ModelEffort = "high"
)

// GapThresholdDays is the gap threshold: the longest month, 31 days, plus 7
// days of slack, for the cron 17 4 3 * *. The report's gap line is stated in
// it, and test/architecture holds it to this one integer literal.
const GapThresholdDays = 38

// historySecretsCheck is the check id of gitleaks on the history: every
// finding of it is a draft advisory naming the commit and the rule only.
const historySecretsCheck = "history-secrets"

// checkNames lists every check id in the coverage table's order, with the name
// the table renders it by.
var checkNames = []struct{ id, name string }{
	{"dependencies", "govulncheck (source) and osv-scanner"},
	{historySecretsCheck, "gitleaks on the full history"},
	{"workflows", "zizmor and actionlint"},
	{"scorecard", "OpenSSF scorecard"},
	{"scorecard-admin", "OpenSSF scorecard checks that need admin read"},
	{"release-integrity", "Release checksums, attestations and signatures"},
	{"image-pull", "Image pulls from GHCR"},
	{"binary-vulnerabilities", "govulncheck on the release binaries"},
	{"image-scan", "trivy on the image and its SBOM"},
	{"image-runtime", "The image's runtime settings"},
	{"hostile-inventories", "The CLI on hostile inventory files"},
	{"fuzzing", "Fuzzing of the committed `Fuzz*` targets"},
	{"supply-chain", "Supply-chain reads"},
	{"tree-review", "The security-audit checklist on the whole tree"},
	{"agent-surface", "What the agents may run"},
}

// whyNotRun maps each why id to the coverage it renders.
var whyNotRun = map[string]string{
	"needs-admin-read": "not run (needs admin read)",
	"no-fuzz-targets":  "not run (no `Fuzz*` targets)",
	"timed-out":        "not run (timed out)",
	"tool-failed":      "not run (tool failed)",
	"results-missing":  "not run (results missing)",
	"read-refused":     "not run (read refused)",
	"image-not-pulled": "not run (image not pulled)",
	"declined":         "not run (declined)",
}

// toolNames is the closed list of tools a result may name.
var toolNames = map[string]bool{
	"actionlint":  true,
	"age":         true,
	"claude-code": true,
	"cosign":      true,
	"gitleaks":    true,
	"go":          true,
	"govulncheck": true,
	"osv-scanner": true,
	"scorecard":   true,
	"trivy":       true,
	"zizmor":      true,
}

// severityRank is the five severities, most severe first: the order the
// findings tables sort by. Any other value is unknown.
var severityRank = map[string]int{
	"critical":      0,
	"high":          1,
	"medium":        2,
	"low":           3,
	"informational": 4,
}

// privateSeverities are the severities that are always private.
var privateSeverities = map[string]bool{
	"critical": true,
	"high":     true,
	"medium":   true,
}

// categories is the closed category vocabulary: true for the four private
// categories. Any other value is unknown, so private.
var categories = map[string]bool{
	"injection":                true,
	"authentication bypass":    true,
	"credential exposure":      true,
	"remote code execution":    true,
	"supply chain":             false,
	"dependency vulnerability": false,
	"configuration":            false,
	"hardening":                false,
	"denial of service":        false,
	"information disclosure":   false,
}

// isCheckID reports whether id is one of the fifteen check ids.
func isCheckID(id string) bool {
	for _, c := range checkNames {
		if c.id == id {
			return true
		}
	}

	return false
}
