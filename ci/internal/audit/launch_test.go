// Pins the launch the audit and its preflight run the model with (ADR 0040's
// model-job amendment, issue #277, "The audit's programs" and "The settings
// file"): the model claude-fable-5-2 at effort high, the six tools the audit
// uses and no other, the permission mode that refuses every tool call no rule
// allows - default, the mode Claude Code 2.1.332 runs under the subprocess
// environment scrub (issue #592) - the settings file read from the checkout copy, the
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

// inlineAllowComment is gitleaks' inline allow comment, built from its two
// halves so that no line of this repository spells it out: the audit's model
// reads the repository, and a line it quotes refuses the whole run (ADR 0040).
const inlineAllowComment = "gitleaks" + ":allow"

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
		// The mode the audit asks for is the mode that runs: Claude Code 2.1.332
		// forces default whenever CLAUDE_CODE_SUBPROCESS_ENV_SCRUB is set, so the
		// launch asks for default and the launcher refuses a session that reports
		// another one (issue #592).
		{"PermissionMode", cfg.PermissionMode, "default"},
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
// why the config carries the rest. The list is compared whole, so a variable the
// caller's environment carries - the ones an earlier step writes to $GITHUB_ENV
// among them - is refused by the same comparison as any other name (issue #277,
// round J nit 5).
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

// TestPromptStatesThePublishCoresTextRules pins the four rules of item 8 (PRD
// #175 milestone 2, ADR 0040's model-job amendment): the user prompt tells the
// model what the publish core refuses in the result's text, so a finding's
// wording does not refuse the whole run. The prompt is also compared character
// by character: it is a text the model copies the style of, so it holds
// printable ASCII alone, as the result it asks for must, and it never carries
// the marker it refuses.
func TestPromptStatesThePublishCoresTextRules(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	const skill = "Read the checkout, the checks and the work area, and write the result file.\n"
	writeFile(t, filepath.Join(root, filepath.FromSlash(skillPath)), skill, 0o444)

	prompt, err := audit.Prompt(root)
	if err != nil {
		t.Fatalf("Prompt(%q) error = %v, want none: the skill is in the checkout copy", root, err)
	}

	for _, rule := range []struct {
		name string
		// words are the words of the rule the prompt must carry.
		words []string
	}{
		{
			name:  "printable ASCII only, with LF and TAB in a multi-line field",
			words: []string{"printable ASCII", "U+0020", "U+007E", "LF and TAB", "refuses the whole run"},
		},
		{
			name:  "no MD5, 32-hex digest, undashed UUID, Rekor entry UUID, SHA-384 or SHA-512 hex, or go.sum h1: hash quoted",
			words: []string{"MD5", "32-hex", "undashed UUID", "Rekor entry UUID", "SHA-384", "SHA-512", "h1:", "refuses the whole run"},
		},
		{
			name:  "a 40- or 64-hex run that decodes to printable ASCII is refused",
			words: []string{"40- or 64-hex", "printable ASCII", "refused"},
		},
		{
			// gitleaks honours its inline allow comment in the text it reads,
			// and the repository itself now holds it: a model that quotes the
			// line refuses the whole run, so the prompt names the marker in
			// words instead of writing it.
			name:  "gitleaks' inline allow comment is never written, only named in words",
			words: []string{"gitleaks' inline allow comment", "in words"},
		},
	} {
		t.Run(rule.name, func(t *testing.T) {
			t.Parallel()

			for _, word := range rule.words {
				if !strings.Contains(prompt.User, word) {
					t.Errorf("the prompt's user text does not name %q, want it to state the rule %q: the publish core refuses a text that breaks it, and the whole run with it (ADR 0040)\nthe prompt: %s",
						word, rule.name, prompt.User)
				}
			}
		})
	}

	// The prompt describes the marker it must not carry: a prompt that wrote
	// gitleaks' inline allow comment literally, in any case, would trip the
	// refusal it states, and the whole run with it.
	for _, text := range []struct {
		what string
		body string
	}{
		{what: "user text", body: prompt.User},
		{what: "system text", body: prompt.System},
	} {
		if strings.Contains(strings.ToLower(text.body), inlineAllowComment) {
			t.Errorf("the prompt's %s holds gitleaks' inline allow comment literally, want it named in words instead: gitleaks honours the marker, so the run refuses its own prompt (ADR 0040)\nthe prompt: %s",
				text.what, text.body)
		}
	}

	// The prompt is a text the model copies the style of: no character of it is
	// outside printable ASCII and the two whitespace characters a field may
	// carry, so it cannot model the quote or the dash a result refuses.
	for _, text := range []struct {
		what string
		body string
	}{
		{what: "user text", body: prompt.User},
		{what: "system text", body: prompt.System},
	} {
		for i, r := range text.body {
			if r == '\n' || r == '\t' || (r >= 0x20 && r <= 0x7e) {
				continue
			}

			t.Errorf("the prompt's %s holds %q at byte %d, want printable ASCII (U+0020 to U+007E) and LF or TAB alone: the model copies the style of what it is asked with", text.what, r, i)

			break
		}
	}
}
