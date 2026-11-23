package audit

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/timothymamo/shift2eu/ci/internal/claudecode"
)

// The audit's launch (ADR 0040, "The settings file").
const (
	// Model is the model the audit and its preflight ask.
	Model = "claude-fable-5-2"
	// effort is the audit's --effort.
	effort = "high"
	// permissionMode refuses every tool call no allow rule matches.
	permissionMode = "dontAsk"
	// searchPath is the CLI's PATH: the sandbox's bwrap and socat are in
	// /usr/bin.
	searchPath = "/usr/bin:/bin"
	// auditTimeout is the audit's own deadline, inside the model job's.
	auditTimeout = 40 * time.Minute
	// settingsFile is the one settings file the CLI loads, in the checkout
	// copy.
	settingsFile = checkoutDir + "/.github/security-audit/claude-settings.json"
	// skillFile is the skill the audit's system prompt is, in the checkout
	// copy.
	skillFile = checkoutDir + "/.claude/skills/security-audit-run/SKILL.md"
	// maxSkill caps the skill's size: a system prompt, not a document.
	maxSkill = 1 << 20
)

// resultTextRules are the lines of the user prompt that state what the publish
// core refuses in the result's text (ADR 0040, "The publish program's core":
// the text rule and the hex rule), so a finding's wording does not refuse the
// whole run.
var resultTextRules = []string{
	"Write every text field of the result file in printable ASCII only: the characters U+0020 to U+007E, and in a multi-line field also LF and TAB. One character outside them, a typographic quote or dash, a non-breaking space or a non-ASCII name, refuses the whole run; write a non-ASCII text you quote in ASCII words instead.",
	"Do not quote an MD5 or other 32-hex digest, an undashed UUID, a Rekor entry UUID, a SHA-384 or SHA-512 hex digest, or a go.sum h1: hash in any field: such a run refuses the whole run. Name the file and line that holds it instead.",
	"A 40- or 64-hex run (a commit SHA or a SHA-256 digest) is accepted only if its bytes do not all decode to printable ASCII: a token or text written as hex is refused, and refuses the whole run.",
}

// auditTools are the built-in tools the audit uses, and no other.
var auditTools = []string{"Read", "Grep", "Glob", "Write", "Edit", "Bash"}

// LaunchConfig returns the launcher's config of the audit and its preflight
// under root: the ADR's model, effort, tools, permission mode and settings
// file, safe mode off, the skill appended to the CLI's system prompt, the work
// area as the working directory, HOME in the state area, the system prompt's
// file in the state area's tmp/, the canary redacted beside the token in the
// launcher's errors, the fixed PATH, and an environment of TMPDIR, the
// subprocess environment scrub and the canary, beside the launcher's own. It
// is built from in alone: nothing of the
// caller's environment reaches it. The caller sets the Transcript.
func LaunchConfig(root string, in Inputs) claudecode.Config {
	return claudecode.Config{
		Path:               under(root, claudePath),
		Token:              in.Token,
		Model:              Model,
		Timeout:            auditTimeout,
		Tools:              append([]string(nil), auditTools...),
		Effort:             effort,
		Settings:           under(root, settingsFile),
		PermissionMode:     permissionMode,
		AppendSystemPrompt: true,
		SafeModeOff:        true,
		WorkDir:            under(root, workDir),
		Home:               under(root, homeDir),
		SearchPath:         searchPath,
		SystemPromptDir:    under(root, stateTmpDir),
		Redact:             []string{in.Canary},
		Env: []string{
			"TMPDIR=" + under(root, stateTmpDir),
			"CLAUDE_CODE_SUBPROCESS_ENV_SCRUB=1",
			"SHIFT2EU_AUDIT_CANARY=" + in.Canary,
		},
	}
}

// Prompt returns what the audit asks the model: the skill, read from the
// checkout copy under root, as the system prompt, and a fixed user prompt that
// names the audit directory's areas and the result file, and states the
// publish core's text and hex rules (resultTextRules). A missing or empty
// skill is an error: the audit never runs with the CLI's prompt alone.
func Prompt(root string) (claudecode.Prompt, error) {
	skill, err := readCapped(under(root, skillFile), maxSkill)
	if err != nil {
		return claudecode.Prompt{}, fmt.Errorf("the audit's skill: %w", err)
	}

	if strings.TrimSpace(string(skill)) == "" {
		return claudecode.Prompt{}, errors.New("the audit's skill is empty")
	}

	user := strings.Join(append([]string{
		"Run the scheduled security audit the system prompt describes.",
		"The repository's checkout, read-only: " + under(root, checkoutDir),
		"The checks' results, read-only: " + under(root, checksDir),
		"Your work area, the one place you may write: " + under(root, workDir),
		"Write the result file to: " + under(root, resultPath),
	}, resultTextRules...), "\n") + "\n"

	return claudecode.Prompt{System: string(skill), User: user}, nil
}
