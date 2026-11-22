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

// auditTools are the built-in tools the audit uses, and no other.
var auditTools = []string{"Read", "Grep", "Glob", "Write", "Edit", "Bash"}

// LaunchConfig returns the launcher's config of the audit and its preflight
// under root: the ADR's model, effort, tools, permission mode and settings
// file, safe mode off, the skill appended to the CLI's system prompt, the work
// area as the working directory, HOME in the state area, the fixed PATH, and
// an environment of TMPDIR, the subprocess environment scrub and the canary,
// beside the launcher's own. It is built from in alone: nothing of the
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
		Env: []string{
			"TMPDIR=" + under(root, stateTmpDir),
			"CLAUDE_CODE_SUBPROCESS_ENV_SCRUB=1",
			"SHIFT2EU_AUDIT_CANARY=" + in.Canary,
		},
	}
}

// Prompt returns what the audit asks the model: the skill, read from the
// checkout copy under root, as the system prompt, and a fixed user prompt that
// names the audit directory's areas and the result file. A missing or empty
// skill is an error: the audit never runs with the CLI's prompt alone.
func Prompt(root string) (claudecode.Prompt, error) {
	skill, err := readCapped(under(root, skillFile), maxSkill)
	if err != nil {
		return claudecode.Prompt{}, fmt.Errorf("the audit's skill: %w", err)
	}

	if strings.TrimSpace(string(skill)) == "" {
		return claudecode.Prompt{}, errors.New("the audit's skill is empty")
	}

	user := strings.Join([]string{
		"Run the scheduled security audit the system prompt describes.",
		"The repository's checkout, read-only: " + under(root, checkoutDir),
		"The checks' results, read-only: " + under(root, checksDir),
		"Your work area, the one place you may write: " + under(root, workDir),
		"Write the result file to: " + under(root, resultPath),
	}, "\n") + "\n"

	return claudecode.Prompt{System: string(skill), User: user}, nil
}
