// Pins the launch the audit and its preflight run the model with (ADR 0040's
// milestone-2 amendment, issue #277, "The audit's programs" and "The settings
// file"): the model claude-fable-5-2 at effort high, the six tools the audit
// uses and no other, the non-interactive permission mode that refuses every
// tool call no rule allows, the settings file read from the checkout copy, the
// work area as the working directory, a fresh HOME and TMPDIR in the state
// area, safe mode off so the settings file applies, and the skill appended to
// the CLI's own system prompt.
//
// The environment is the ADR's allow list and nothing else: the launcher adds
// the token, HOME, PATH and its four switches, and these entries are the rest -
// TMPDIR, the subprocess environment scrub and the step's canary. Nothing the
// caller's own environment holds, the variables an earlier step could write to
// $GITHUB_ENV among them, reaches it: the config is built from the step's
// inputs, never from the environment.
package audit_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/internal/audit"
)

// skillPath is the skill the audit's system prompt is, under a root that stands
// in for /: the copy in the checkout the model job made.
const skillPath = "srv/audit/checkout/.claude/skills/security-audit-run/SKILL.md"

// TestLaunchConfigIsTheADRsLaunch pins every field of the launcher's config the
// ADR fixes for the audit.
func TestLaunchConfigIsTheADRsLaunch(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	inputs := testInputs(root)
	cfg := audit.LaunchConfig(root, inputs)

	// The ADR's paths, rooted at the stand-in root the library is handed.
	for _, field := range []struct {
		name string
		got  string
		want string
	}{
		{"Path", cfg.Path, filepath.Join(root, "srv/audit/state/bin/claude")},
		{"Settings", cfg.Settings, filepath.Join(root, "srv/audit/checkout/.github/security-audit/claude-settings.json")},
		{"WorkDir", cfg.WorkDir, filepath.Join(root, "srv/audit/work")},
		{"Home", cfg.Home, filepath.Join(root, "srv/audit/state/home")},
		// The CLI writes a long system prompt to a file in this directory
		// before it reads it back: the state area's tmp/, never the work area
		// the model writes in.
		{"SystemPromptDir", cfg.SystemPromptDir, filepath.Join(root, "srv/audit/state/tmp")},
		{"SearchPath", cfg.SearchPath, "/usr/bin:/bin"},
		{"Token", cfg.Token, inputs.Token},
		{"Model", cfg.Model, "claude-fable-5-2"},
		{"Effort", cfg.Effort, "high"},
		{"PermissionMode", cfg.PermissionMode, "dontAsk"},
	} {
		if field.got != field.want {
			t.Errorf("the audit launch's %s is %q, want %q", field.name, field.got, field.want)
		}
	}

	if want := []string{"Read", "Grep", "Glob", "Write", "Edit", "Bash"}; !slices.Equal(cfg.Tools, want) {
		t.Errorf("the audit launch's tools are %v, want %v: no WebFetch, WebSearch or Task, and no other (ADR 0040)", cfg.Tools, want)
	}

	// The launcher's errors hide the canary beside the token: the canary and
	// nothing else, so nothing of the caller's or the model's making is redacted
	// out of a report the audit's summary prints.
	if want := []string{inputs.Canary}; !slices.Equal(cfg.Redact, want) {
		t.Errorf("the audit launch's Redact holds %v, want %v: the canary is the one value the launcher hides beside the token (ADR 0040)", cfg.Redact, want)
	}

	// Safe mode turns the settings file's customisations off; the audit's
	// settings must apply, and each thing safe mode would turn off is off by
	// another flag or key of that file.
	if !cfg.SafeModeOff {
		t.Error("the audit launch leaves safe mode on; the settings file would not apply (ADR 0040)")
	}

	// The CLI's own system prompt explains the tools the audit uses, so the
	// skill is appended to it rather than replacing it.
	if !cfg.AppendSystemPrompt {
		t.Error("the audit launch replaces the CLI's system prompt; the skill is appended to it (ADR 0040)")
	}

	// A run without its own deadline would hold the job open until the
	// workflow's timeout.
	if cfg.Timeout <= 0 {
		t.Errorf("the audit launch's timeout is %s, want a deadline of its own", cfg.Timeout)
	}
}

// TestLaunchConfigEnvIsTheADRsAllowListAlone pins the config's environment
// entries: TMPDIR in the state area, the subprocess environment scrub and the
// step's canary, and nothing else. The launcher adds the token, HOME, PATH and
// its four switches itself; a name it sets itself cannot be an entry, which is
// why the config carries the rest.
func TestLaunchConfigEnvIsTheADRsAllowListAlone(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	inputs := testInputs(root)
	cfg := audit.LaunchConfig(root, inputs)

	want := []string{
		"TMPDIR=" + filepath.Join(root, "srv/audit/state/tmp"),
		"CLAUDE_CODE_SUBPROCESS_ENV_SCRUB=1",
		"SHIFT2EU_AUDIT_CANARY=" + inputs.Canary,
	}

	got := slices.Clone(cfg.Env)
	slices.Sort(got)
	slices.Sort(want)

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("the audit launch's environment entries (-want +got):\n%s", diff)
	}
}

// TestLaunchConfigHoldsNoneOfTheCallersVariables pins the allow list against
// the variables a step's environment can carry: an earlier step writes
// $GITHUB_ENV, which the runner then exports into every later step, and the
// managed-settings path points the CLI at another policy file. None of them is
// an entry of the child's environment, and no entry is the caller's to add: the
// config is built from the step's inputs, which the launcher is handed, and the
// launcher's own environment carries the token, HOME, PATH and its four
// switches and nothing of the process it runs in (ci/internal/claudecode pins
// that whole list).
//
// The test cannot set the process's environment to prove it - test/lint refuses
// t.Setenv, which changes the whole test binary's environment - so it holds the
// entries to the audit's own list and names the caller's variables one by one.
func TestLaunchConfigHoldsNoneOfTheCallersVariables(t *testing.T) {
	t.Parallel()

	callers := []string{
		"ANTHROPIC_API_KEY",
		"ANTHROPIC_BASE_URL",
		"HTTPS_PROXY",
		"NODE_OPTIONS",
		"LD_PRELOAD",
		"CLAUDE_CODE_MANAGED_SETTINGS_PATH",
		"SHIFT2EU_AUDIT_SENTINEL",
	}

	root := t.TempDir()
	cfg := audit.LaunchConfig(root, testInputs(root))

	own := map[string]bool{
		"TMPDIR":                           true,
		"CLAUDE_CODE_SUBPROCESS_ENV_SCRUB": true,
		"SHIFT2EU_AUDIT_CANARY":            true,
	}

	for _, entry := range cfg.Env {
		name, _, _ := strings.Cut(entry, "=")

		if slices.Contains(callers, name) {
			t.Errorf("the audit launch's environment holds %s, which is the caller's to set: the launcher's environment is the ADR's allow list and inherits nothing (ADR 0040)", name)
		}

		if !own[name] {
			t.Errorf("the audit launch's environment holds %s, want one of TMPDIR, CLAUDE_CODE_SUBPROCESS_ENV_SCRUB and SHIFT2EU_AUDIT_CANARY: the rest of the allow list is the launcher's own (ADR 0040)", name)
		}
	}
}

// TestPromptIsTheSkillAndTheADRsPaths pins what the audit asks the model: the
// skill's text, read from the checkout copy under the root, as the system
// prompt, and a user prompt that names the audit directory's three areas and
// the result file the report waits for. The paths are the tree's own, under the
// root the library stands / in.
func TestPromptIsTheSkillAndTheADRsPaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	const skill = "Read the checkout, the checks and the work area, and write the result file.\n"
	writeFile(t, filepath.Join(root, filepath.FromSlash(skillPath)), skill, 0o444)

	prompt, err := audit.Prompt(root)
	if err != nil {
		t.Fatalf("Prompt(%q) error = %v, want none: the skill is in the checkout copy", root, err)
	}

	if prompt.System != skill {
		t.Errorf("the prompt's system text is %q, want the skill's text %q: the skill reaches the model only as the system prompt (ADR 0040)", prompt.System, skill)
	}

	for _, want := range []string{
		filepath.Join(root, "srv/audit/checkout"),
		filepath.Join(root, "srv/audit/checks"),
		filepath.Join(root, "srv/audit/work"),
		filepath.Join(root, "srv/audit/work/result.json"),
	} {
		if !strings.Contains(prompt.User, want) {
			t.Errorf("the prompt's user text is %q, want it to name %q: the model is told where the audit directory's areas and the result file are (ADR 0040)", prompt.User, want)
		}
	}
}

// TestPromptNeedsTheSkillsCopy pins the other side: without the checkout copy's
// skill there is no system prompt, so the run fails before the CLI starts rather
// than asking the model with the CLI's own prompt alone.
func TestPromptNeedsTheSkillsCopy(t *testing.T) {
	t.Parallel()

	if _, err := audit.Prompt(t.TempDir()); err == nil {
		t.Errorf("Prompt() = nil, want an error: the skill is not in the checkout copy")
	}
}
