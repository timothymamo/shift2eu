package claudecode_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/internal/claudecode"
)

// missingCLI is an absolute path no CLI lives at: a run that got as far as
// starting it fails with "start the Claude Code CLI", which a refused config
// must never reach.
const missingCLI = "/nonexistent/claude-code/package/claude"

// TestArgsTakeTheModelAndTheTools pins the two parameters the shared launcher
// takes (issue #277, ADR 0040's amendment): --model is the config's model, and
// --tools is its tools joined with ",", with no tools at all, --tools "", when
// it names none. Every other argument is the reviewer's launch as ADR 0033
// fixed it, in the same order, with the empty MCP configuration ADR 0040's
// amendment adds for both callers: --strict-mcp-config alone reads a
// configuration it is never given.
func TestArgsTakeTheModelAndTheTools(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		model     string
		tools     []string
		wantTools string
	}{
		{name: "nil tools are no tools", model: "claude-opus-5-5", tools: nil, wantTools: ""},
		{name: "an empty list is no tools", model: "claude-opus-5-5", tools: []string{}, wantTools: ""},
		{name: "one tool", model: "claude-fable-5-2", tools: []string{"Read"}, wantTools: "Read"},
		{name: "several tools keep their order", model: "claude-fable-5-2", tools: []string{"Read", "Grep", "Glob"}, wantTools: "Read,Grep,Glob"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := claudecode.Args(claudecode.Config{Path: missingCLI, Model: tc.model, Tools: tc.tools}, "/prompt/system-prompt.md")

			want := []string{
				"-p",
				"--model", tc.model,
				"--output-format", "json",
				"--safe-mode",
				"--tools", tc.wantTools,
				"--setting-sources", "",
				"--strict-mcp-config",
				"--mcp-config", `{"mcpServers":{}}`,
				"--disable-slash-commands",
				"--no-session-persistence",
				"--system-prompt-file", "/prompt/system-prompt.md",
			}

			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("the CLI's arguments (-want +got):\n%s", diff)
			}
		})
	}
}

// TestNewKeepsItsOwnCopyOfTheTools pins that a caller changing its slice after
// New cannot change the tools of a launcher it already built: the launcher is
// built with a valid tool, the slice then holds a name Ask would refuse, and
// Ask still gets as far as starting the (missing) CLI.
func TestNewKeepsItsOwnCopyOfTheTools(t *testing.T) {
	t.Parallel()

	tools := []string{"Read"}
	cli := claudecode.New(claudecode.Config{Path: missingCLI, Model: "claude-fable-5-2", Tools: tools, Timeout: time.Minute})
	tools[0] = "Bash(curl:*)"

	_, err := cli.Ask(t.Context(), claudecode.Prompt{System: "s", User: "u"})
	if !errors.Is(err, claudecode.ErrModel) || !strings.Contains(err.Error(), "start the Claude Code CLI") {
		t.Fatalf("Ask = %v, want the missing CLI's start failure: the launcher kept the tool it was built with", err)
	}
}

// TestAskRefusesAConfigOfTheWrongShape pins that a model or a tool name of
// the wrong shape fails the run with ErrModel before the CLI starts: no run
// asks the CLI's default model, and a permission rule or a list in one name
// never reaches --tools, where the CLI would read it as something else.
func TestAskRefusesAConfigOfTheWrongShape(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		model string
		tools []string
	}{
		{name: "an empty model", model: ""},
		{name: "a model with a space", model: "claude opus"},
		{name: "a model with a shell character", model: "claude;opus"},
		{name: "a model over 64 bytes", model: strings.Repeat("a", 65)},
		{name: "an empty tool name", model: "claude-fable-5-2", tools: []string{""}},
		{name: "a permission rule as a tool", model: "claude-fable-5-2", tools: []string{"Bash(git log:*)"}},
		{name: "a list in one tool name", model: "claude-fable-5-2", tools: []string{"Read,Bash"}},
		{name: "a tool name with a digit", model: "claude-fable-5-2", tools: []string{"Read2"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cli := claudecode.New(claudecode.Config{Path: missingCLI, Model: tc.model, Tools: tc.tools, Timeout: time.Minute})

			text, err := cli.Ask(t.Context(), claudecode.Prompt{System: "s", User: "u"})
			if !errors.Is(err, claudecode.ErrModel) {
				t.Fatalf("Ask = %q, %v; want an error wrapping ErrModel", text, err)
			}

			if strings.Contains(err.Error(), "start the Claude Code CLI") {
				t.Errorf("Ask = %v; the config is refused before the CLI starts, not by its failure to start", err)
			}
		})
	}
}
