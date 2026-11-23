// Pins the launch the shared launcher gives the Claude Code CLI for the fields
// ADR 0040's model-job amendment adds beside the model and the tools (issue
// #277): the effort, the settings file, the permission mode, the appended
// system prompt, safe mode left out, the working directory, HOME, the extra
// environment entries, the CLI's PATH and the streaming transcript. The
// reviewer's launch, which sets none of them, is the zero value of every one,
// and stays what it was.
package claudecode_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/internal/claudecode"
)

const (
	// auditModel is the model ADR 0040's model-job amendment gives the audit's launch: a
	// model name the launcher accepts, and none of the reviewer's.
	auditModel = "claude-fable-5-2"

	// auditTools are the built-in tools ADR 0040's model-job amendment gives the audit:
	// no WebFetch, WebSearch, Task or any other.
	auditTools = "Read,Grep,Glob,Write,Edit,Bash"

	// auditSettings is the settings file ADR 0040's model-job amendment names: the one
	// the audit's checkout copy holds, the only settings file the CLI loads.
	auditSettings = "/srv/audit/checkout/.github/security-audit/claude-settings.json"

	// auditWorkDir and auditHome are the two directories the audit's launch
	// fixes: the CLI's working directory and its HOME.
	auditWorkDir = "/srv/audit/work"
	auditHome    = "/srv/audit/state/home"

	// subscriptionToken is an obvious fake, never a real credential (CLAUDE.md
	// rule 3): a config a test builds to reach the CLI carries it, since a
	// launch refuses an empty token before the CLI starts (round L).
	subscriptionToken = "fake-subscription-value-123"
)

// TestArgsPrescribeEachConfigField pins the argument vector ADR 0040's
// model-job amendment fixes for every field the launcher gained, in the order it fixes:
// `-p --model <Model>` [`--effort`] `--output-format json|stream-json`
// [`--verbose`] [`--safe-mode`] `--tools <list>` `--setting-sources ""`
// [`--settings`] [`--permission-mode`] `--strict-mcp-config --mcp-config
// {"mcpServers":{}}` `--disable-slash-commands` `--no-session-persistence`
// `--system-prompt-file|--append-system-prompt-file <file>`. A field at its
// zero value adds no flag, so the reviewer's launch is the same vector it was,
// with the empty MCP configuration the amendment adds for both callers.
func TestArgsPrescribeEachConfigField(t *testing.T) {
	t.Parallel()

	// reviewer is the vector a config with none of the new fields asks for: the
	// reviewer's launch, byte for byte, with the empty MCP configuration.
	reviewer := []string{
		"-p",
		"--model", auditModel,
		"--output-format", "json",
		"--safe-mode",
		"--tools", "",
		"--setting-sources", "",
		"--strict-mcp-config",
		"--mcp-config", `{"mcpServers":{}}`,
		"--disable-slash-commands",
		"--no-session-persistence",
		"--system-prompt-file", "/prompt/system-prompt.md",
	}

	cases := []struct {
		name   string
		config claudecode.Config
		want   []string
	}{
		{
			name:   "no new field is the reviewer's launch",
			config: claudecode.Config{Path: missingCLI, Model: auditModel},
			want:   reviewer,
		},
		{
			name:   "the effort follows the model",
			config: claudecode.Config{Path: missingCLI, Model: auditModel, Effort: "high"},
			want: []string{
				"-p",
				"--model", auditModel,
				"--effort", "high",
				"--output-format", "json",
				"--safe-mode",
				"--tools", "",
				"--setting-sources", "",
				"--strict-mcp-config",
				"--mcp-config", `{"mcpServers":{}}`,
				"--disable-slash-commands",
				"--no-session-persistence",
				"--system-prompt-file", "/prompt/system-prompt.md",
			},
		},
		{
			name:   "the settings file follows the empty setting sources",
			config: claudecode.Config{Path: missingCLI, Model: auditModel, Settings: auditSettings},
			want: []string{
				"-p",
				"--model", auditModel,
				"--output-format", "json",
				"--safe-mode",
				"--tools", "",
				"--setting-sources", "",
				"--settings", auditSettings,
				"--strict-mcp-config",
				"--mcp-config", `{"mcpServers":{}}`,
				"--disable-slash-commands",
				"--no-session-persistence",
				"--system-prompt-file", "/prompt/system-prompt.md",
			},
		},
		{
			name:   "the permission mode follows the settings file",
			config: claudecode.Config{Path: missingCLI, Model: auditModel, PermissionMode: "dontAsk"},
			want: []string{
				"-p",
				"--model", auditModel,
				"--output-format", "json",
				"--safe-mode",
				"--tools", "",
				"--setting-sources", "",
				"--permission-mode", "dontAsk",
				"--strict-mcp-config",
				"--mcp-config", `{"mcpServers":{}}`,
				"--disable-slash-commands",
				"--no-session-persistence",
				"--system-prompt-file", "/prompt/system-prompt.md",
			},
		},
		{
			name:   "leaving safe mode out drops its one flag",
			config: claudecode.Config{Path: missingCLI, Model: auditModel, SafeModeOff: true},
			want: []string{
				"-p",
				"--model", auditModel,
				"--output-format", "json",
				"--tools", "",
				"--setting-sources", "",
				"--strict-mcp-config",
				"--mcp-config", `{"mcpServers":{}}`,
				"--disable-slash-commands",
				"--no-session-persistence",
				"--system-prompt-file", "/prompt/system-prompt.md",
			},
		},
		{
			name:   "an appended system prompt replaces the flag that replaces the CLI's prompt",
			config: claudecode.Config{Path: missingCLI, Model: auditModel, AppendSystemPrompt: true},
			want: []string{
				"-p",
				"--model", auditModel,
				"--output-format", "json",
				"--safe-mode",
				"--tools", "",
				"--setting-sources", "",
				"--strict-mcp-config",
				"--mcp-config", `{"mcpServers":{}}`,
				"--disable-slash-commands",
				"--no-session-persistence",
				"--append-system-prompt-file", "/prompt/system-prompt.md",
			},
		},
		{
			name:   "the working directory, HOME and PATH take no argument",
			config: claudecode.Config{Path: missingCLI, Model: auditModel, WorkDir: auditWorkDir, Home: auditHome, SearchPath: "/usr/bin:/bin"},
			want:   reviewer,
		},
		{
			name:   "extra environment entries and the directories take no argument",
			config: claudecode.Config{Path: missingCLI, Model: auditModel, Env: []string{"TMPDIR=/srv/audit/state/tmp"}, WorkDir: auditWorkDir, Home: auditHome},
			want:   reviewer,
		},
		{
			name:   "a transcript streams the run",
			config: claudecode.Config{Path: missingCLI, Model: auditModel, Transcript: &bytes.Buffer{}},
			want: []string{
				"-p",
				"--model", auditModel,
				"--output-format", "stream-json",
				"--verbose",
				"--safe-mode",
				"--tools", "",
				"--setting-sources", "",
				"--strict-mcp-config",
				"--mcp-config", `{"mcpServers":{}}`,
				"--disable-slash-commands",
				"--no-session-persistence",
				"--system-prompt-file", "/prompt/system-prompt.md",
			},
		},
		{
			name: "the audit's whole launch",
			config: claudecode.Config{
				Path:               missingCLI,
				Model:              auditModel,
				Tools:              strings.Split(auditTools, ","),
				Effort:             "high",
				Settings:           auditSettings,
				PermissionMode:     "dontAsk",
				AppendSystemPrompt: true,
				SafeModeOff:        true,
				WorkDir:            auditWorkDir,
				Home:               auditHome,
				Env:                []string{"TMPDIR=/srv/audit/state/tmp", "CLAUDE_CODE_SUBPROCESS_ENV_SCRUB=1"},
				SearchPath:         "/usr/bin:/bin",
				Transcript:         &bytes.Buffer{},
			},
			want: []string{
				"-p",
				"--model", auditModel,
				"--effort", "high",
				"--output-format", "stream-json",
				"--verbose",
				"--tools", auditTools,
				"--setting-sources", "",
				"--settings", auditSettings,
				"--permission-mode", "dontAsk",
				"--strict-mcp-config",
				"--mcp-config", `{"mcpServers":{}}`,
				"--disable-slash-commands",
				"--no-session-persistence",
				"--append-system-prompt-file", "/prompt/system-prompt.md",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := claudecode.Args(tc.config, "/prompt/system-prompt.md")

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("the CLI's arguments (-want +got):\n%s", diff)
			}
		})
	}
}

// TestEnvIsTheAllowListAlone pins the CLI's whole environment (ADR 0040's
// model-job amendment): the token, HOME, PATH, the four switches and the config's own
// entries, each a name and a value, and nothing else. The slice is the whole of
// cmd.Env, so a variable of the caller's reaching the CLI would have to be here;
// and HOME, PATH and the switches are the launcher's, which a config cannot
// override.
func TestEnvIsTheAllowListAlone(t *testing.T) {
	t.Parallel()

	// token is an obvious fake, never a real credential (CLAUDE.md rule 3).
	const subscriptionValue = "fake-subscription-value-123"

	allowList := func(entries ...string) []string {
		return append([]string{
			"CLAUDE_CODE_OAUTH_TOKEN=" + subscriptionValue,
			"HOME=" + auditHome,
			"PATH=/usr/local/bin:/usr/bin:/bin",
			"DISABLE_AUTOUPDATER=1",
			"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
			"DISABLE_TELEMETRY=1",
			"DISABLE_ERROR_REPORTING=1",
		}, entries...)
	}

	cases := []struct {
		name   string
		config claudecode.Config
		home   string
		want   []string
	}{
		{
			name:   "the reviewer's launch is the allow list and nothing else",
			config: claudecode.Config{Path: missingCLI, Model: "claude-opus-5-5"},
			home:   "/run/claudecode-home-1",
			want: []string{
				"CLAUDE_CODE_OAUTH_TOKEN=" + subscriptionValue,
				"HOME=/run/claudecode-home-1",
				"PATH=/usr/local/bin:/usr/bin:/bin",
				"DISABLE_AUTOUPDATER=1",
				"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
				"DISABLE_TELEMETRY=1",
				"DISABLE_ERROR_REPORTING=1",
			},
		},
		{
			name:   "the audit's HOME it was given",
			config: claudecode.Config{Path: missingCLI, Model: auditModel, Home: auditHome, WorkDir: auditWorkDir},
			home:   "/run/claudecode-home-1",
			want:   allowList(),
		},
		{
			name:   "the audit's fixed PATH",
			config: claudecode.Config{Path: missingCLI, Model: auditModel, SearchPath: "/usr/bin:/bin"},
			home:   auditHome,
			want: []string{
				"CLAUDE_CODE_OAUTH_TOKEN=" + subscriptionValue,
				"HOME=" + auditHome,
				"PATH=/usr/bin:/bin",
				"DISABLE_AUTOUPDATER=1",
				"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
				"DISABLE_TELEMETRY=1",
				"DISABLE_ERROR_REPORTING=1",
			},
		},
		{
			name: "the entries the audit adds, in the order it gives them",
			config: claudecode.Config{
				Path:  missingCLI,
				Model: auditModel,
				Home:  auditHome,
				Env: []string{
					"TMPDIR=/srv/audit/state/tmp",
					"CLAUDE_CODE_SUBPROCESS_ENV_SCRUB=1",
					"SHIFT2EU_AUDIT_CANARY=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
				},
			},
			home: auditHome,
			want: allowList(
				"TMPDIR=/srv/audit/state/tmp",
				"CLAUDE_CODE_SUBPROCESS_ENV_SCRUB=1",
				"SHIFT2EU_AUDIT_CANARY=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tc.config.Token = subscriptionValue

			got := claudecode.Env(tc.config, tc.home)

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("the CLI's environment (-want +got):\n%s", diff)
			}
		})
	}
}

// TestAskRefusesTheNewFieldsAtTheirZeroShape pins what a config of a shape ADR
// 0040's model-job amendment does not allow fails with: ErrModel, before the CLI starts,
// like a relative path or a model name of the wrong shape. A permission mode
// other than dontAsk or plan is the one that matters most: bypassPermissions,
// the CLI's own name for running a call no rule matches, is never passed on.
func TestAskRefusesTheNewFieldsAtTheirZeroShape(t *testing.T) {
	t.Parallel()

	// outsidePath is a path that exists, of which no part is a directory: the
	// refusals below that are about a path's shape use its own names.
	outsidePath := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(outsidePath, []byte("not a directory\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", outsidePath, err)
	}

	missingDir := filepath.Join(t.TempDir(), "missing")

	cases := []struct {
		name  string
		field string
		apply func(*claudecode.Config)
	}{
		{
			name:  "the permission mode bypassPermissions",
			field: "permission mode",
			apply: func(c *claudecode.Config) { c.PermissionMode = "bypassPermissions" },
		},
		{
			name:  "the permission mode of the CLI's own spelling in another case",
			field: "permission mode",
			apply: func(c *claudecode.Config) { c.PermissionMode = "dontask" },
		},
		{
			name:  "the permission mode acceptEdits",
			field: "permission mode",
			apply: func(c *claudecode.Config) { c.PermissionMode = "acceptEdits" },
		},
		{
			name:  "a permission mode with a space in it",
			field: "permission mode",
			apply: func(c *claudecode.Config) { c.PermissionMode = "plan --verbose" },
		},
		{
			name:  "an effort outside the five the CLI takes",
			field: "effort",
			apply: func(c *claudecode.Config) { c.Effort = "turbo" },
		},
		{
			name:  "a settings file of a relative path",
			field: "settings",
			apply: func(c *claudecode.Config) { c.Settings = ".github/security-audit/claude-settings.json" },
		},
		{
			name:  "a working directory of a relative path",
			field: "working directory",
			apply: func(c *claudecode.Config) { c.WorkDir = "work" },
		},
		{
			name:  "a working directory that is no directory",
			field: "working directory",
			apply: func(c *claudecode.Config) { c.WorkDir = outsidePath },
		},
		{
			name:  "a HOME of a relative path",
			field: "HOME",
			apply: func(c *claudecode.Config) { c.Home = "home" },
		},
		{
			name:  "a HOME that does not exist",
			field: "HOME",
			apply: func(c *claudecode.Config) { c.Home = missingDir },
		},
		{
			name:  "an entry that sets the token",
			field: "CLAUDE_CODE_OAUTH_TOKEN",
			apply: func(c *claudecode.Config) { c.Env = []string{"CLAUDE_CODE_OAUTH_TOKEN=another"} },
		},
		{
			name:  "an entry that sets HOME",
			field: "HOME",
			apply: func(c *claudecode.Config) { c.Env = []string{"HOME=/root"} },
		},
		{
			name:  "an entry that sets PATH",
			field: "PATH",
			apply: func(c *claudecode.Config) { c.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin"} },
		},
		{
			name:  "an entry that turns the autoupdater back on",
			field: "DISABLE_AUTOUPDATER",
			apply: func(c *claudecode.Config) { c.Env = []string{"DISABLE_AUTOUPDATER=0"} },
		},
		{
			name:  "an entry that turns non-essential traffic back on",
			field: "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC",
			apply: func(c *claudecode.Config) { c.Env = []string{"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=0"} },
		},
		{
			name:  "an entry that turns telemetry back on",
			field: "DISABLE_TELEMETRY",
			apply: func(c *claudecode.Config) { c.Env = []string{"DISABLE_TELEMETRY=0"} },
		},
		{
			name:  "an entry that turns error reporting back on",
			field: "DISABLE_ERROR_REPORTING",
			apply: func(c *claudecode.Config) { c.Env = []string{"DISABLE_ERROR_REPORTING=0"} },
		},
		{
			name:  "a system prompt's directory of a relative path",
			field: "system prompt",
			apply: func(c *claudecode.Config) { c.SystemPromptDir = "state/tmp" },
		},
		{
			name:  "a system prompt's directory that does not exist",
			field: "system prompt",
			apply: func(c *claudecode.Config) { c.SystemPromptDir = missingDir },
		},

		// The config's two required fields are refused at their zero shape
		// (issue #277, round L, AI 1): a run with no token would hand the CLI
		// an empty CLAUDE_CODE_OAUTH_TOKEN, and one with no deadline would run
		// with none, so each is refused before the CLI starts, naming the field
		// as the messages above name theirs. A negative timeout is no deadline
		// either: the run's context is over before the CLI starts.
		{
			name:  "an empty token",
			field: "token",
			apply: func(c *claudecode.Config) { c.Token = "" },
		},
		{
			name:  "a timeout of zero",
			field: "timeout",
			apply: func(c *claudecode.Config) { c.Timeout = 0 },
		},
		{
			name:  "a negative timeout",
			field: "timeout",
			apply: func(c *claudecode.Config) { c.Timeout = -time.Minute },
		},

		// An environment entry's value is checked as the CLI's PATH is (issue
		// #277, round L, AI 2): a NUL or a newline in it ends the entry where
		// the CLI reads it, so the value the launcher set is not the value the
		// CLI's environment holds. Each is refused before the CLI starts.
		{
			name:  "an entry with a NUL in its value",
			field: "environment entry",
			apply: func(c *claudecode.Config) { c.Env = []string{"TMPDIR=/srv/audit/work/x\x00"} },
		},
		{
			name:  "an entry with a newline in its value",
			field: "environment entry",
			apply: func(c *claudecode.Config) { c.Env = []string{"TMPDIR=/srv/audit/work/x\n"} },
		},

		// The variables that change the CLI's endpoint or its policy are the
		// caller's to set nowhere: the launcher's environment is the ADR's allow
		// list, and an entry that pointed the CLI at another endpoint, loaded
		// another managed-settings file or preloaded a library would take the
		// audit's run out of the ADR's containment (issue #277, audit A10).
		{
			name:  "an entry that sets an API key",
			field: "ANTHROPIC_API_KEY",
			apply: func(c *claudecode.Config) { c.Env = []string{"ANTHROPIC_API_KEY=sk-ant-invented"} },
		},
		{
			name:  "an entry that sets an auth token",
			field: "ANTHROPIC_AUTH_TOKEN",
			apply: func(c *claudecode.Config) { c.Env = []string{"ANTHROPIC_AUTH_TOKEN=invented"} },
		},
		{
			name:  "an entry that points the CLI at another endpoint",
			field: "ANTHROPIC_BASE_URL",
			apply: func(c *claudecode.Config) { c.Env = []string{"ANTHROPIC_BASE_URL=https://example.com"} },
		},
		{
			name:  "an entry that names another settings file",
			field: "CLAUDE_CODE_MANAGED_SETTINGS_PATH",
			apply: func(c *claudecode.Config) {
				c.Env = []string{"CLAUDE_CODE_MANAGED_SETTINGS_PATH=/srv/audit/work/x.json"}
			},
		},
		{
			name:  "an entry that loads a library into the CLI's node",
			field: "NODE_OPTIONS",
			apply: func(c *claudecode.Config) { c.Env = []string{"NODE_OPTIONS=--require /srv/audit/work/x.js"} },
		},
		{
			name:  "an entry that preloads a library",
			field: "LD_PRELOAD",
			apply: func(c *claudecode.Config) { c.Env = []string{"LD_PRELOAD=/srv/audit/work/x.so"} },
		},
		{
			name:  "an entry that names another library path",
			field: "LD_LIBRARY_PATH",
			apply: func(c *claudecode.Config) { c.Env = []string{"LD_LIBRARY_PATH=/srv/audit/work"} },
		},
		{
			name:  "an entry that routes the CLI's HTTPS through a proxy",
			field: "HTTPS_PROXY",
			apply: func(c *claudecode.Config) { c.Env = []string{"HTTPS_PROXY=http://192.0.2.10:3128"} },
		},
		{
			name:  "an entry that routes the CLI's HTTP through a proxy",
			field: "HTTP_PROXY",
			apply: func(c *claudecode.Config) { c.Env = []string{"HTTP_PROXY=http://192.0.2.10:3128"} },
		},
		{
			name:  "an entry that switches the CLI to another provider",
			field: "CLAUDE_CODE_USE_BEDROCK",
			apply: func(c *claudecode.Config) { c.Env = []string{"CLAUDE_CODE_USE_BEDROCK=1"} },
		},
		{
			name:  "an entry that switches the CLI to a third provider",
			field: "CLAUDE_CODE_USE_VERTEX",
			apply: func(c *claudecode.Config) { c.Env = []string{"CLAUDE_CODE_USE_VERTEX=\"1\""} },
		},

		// The proxy the CLI reads in its lower case, and in the upper case of
		// the two macOS spells: each routes the CLI's traffic through a host the
		// audit does not know (issue #277, round D finding 5).
		{
			name:  "an entry that routes the CLI's HTTPS through a lower-case proxy",
			field: "https_proxy",
			apply: func(c *claudecode.Config) { c.Env = []string{"https_proxy=http://192.0.2.10:3128"} },
		},
		{
			name:  "an entry that routes the CLI's HTTP through a lower-case proxy",
			field: "http_proxy",
			apply: func(c *claudecode.Config) { c.Env = []string{"http_proxy=http://192.0.2.10:3128"} },
		},
		{
			name:  "an entry that routes every protocol through a lower-case proxy",
			field: "all_proxy",
			apply: func(c *claudecode.Config) { c.Env = []string{"all_proxy=http://192.0.2.10:3128"} },
		},
		{
			name:  "an entry that names the hosts a lower-case proxy skips",
			field: "no_proxy",
			apply: func(c *claudecode.Config) { c.Env = []string{"no_proxy=example.com"} },
		},
		{
			name:  "an entry that routes every protocol through an upper-case proxy",
			field: "ALL_PROXY",
			apply: func(c *claudecode.Config) { c.Env = []string{"ALL_PROXY=http://192.0.2.10:3128"} },
		},
		{
			name:  "an entry that names the hosts an upper-case proxy skips",
			field: "NO_PROXY",
			apply: func(c *claudecode.Config) { c.Env = []string{"NO_PROXY=example.com"} },
		},

		// The variables that turn the CLI's own transport's checks off, or load
		// code into the process before the CLI's first line runs (issue #277,
		// round D finding 5).
		{
			name:  "an entry that turns the CLI's TLS verification off",
			field: "NODE_TLS_REJECT_UNAUTHORIZED",
			apply: func(c *claudecode.Config) { c.Env = []string{"NODE_TLS_REJECT_UNAUTHORIZED=0"} },
		},
		{
			name:  "an entry that adds a certificate authority to the CLI's node",
			field: "NODE_EXTRA_CA_CERTS",
			apply: func(c *claudecode.Config) { c.Env = []string{"NODE_EXTRA_CA_CERTS=/srv/audit/work/ca.pem"} },
		},
		{
			name:  "an entry that moves the CLI's configuration directory",
			field: "CLAUDE_CONFIG_DIR",
			apply: func(c *claudecode.Config) { c.Env = []string{"CLAUDE_CONFIG_DIR=/srv/audit/work/claude"} },
		},
		{
			name:  "an entry that adds a header to every request the CLI makes",
			field: "ANTHROPIC_CUSTOM_HEADERS",
			apply: func(c *claudecode.Config) { c.Env = []string{"ANTHROPIC_CUSTOM_HEADERS=X-Forwarded-For: 192.0.2.10"} },
		},
		{
			name:  "an entry that loads an audit library into every process",
			field: "LD_AUDIT",
			apply: func(c *claudecode.Config) { c.Env = []string{"LD_AUDIT=/srv/audit/work/audit.so"} },
		},

		// The config's environment is an allow list, not a deny list of the
		// names someone thought of (issue #277, round F finding 1): a config
		// may add TMPDIR, CLAUDE_CODE_SUBPROCESS_ENV_SCRUB and
		// SHIFT2EU_AUDIT_CANARY, the three ADR 0040's model-job amendment gives the
		// audit, and no other name at all. The six below got past the deny
		// list, and each changes what the CLI or its children run or trust.
		{
			name:  "an entry that makes every shell the CLI starts run a script",
			field: "BASH_ENV",
			apply: func(c *claudecode.Config) { c.Env = []string{"BASH_ENV=/srv/audit/work/x.sh"} },
		},
		{
			name:  "an entry that makes every shell the CLI starts run its startup files",
			field: "ENV",
			apply: func(c *claudecode.Config) { c.Env = []string{"ENV=/srv/audit/work/x.sh"} },
		},
		{
			name:  "an entry that prefixes every command the CLI's shell runs",
			field: "CLAUDE_CODE_SHELL_PREFIX",
			apply: func(c *claudecode.Config) { c.Env = []string{"CLAUDE_CODE_SHELL_PREFIX=/srv/audit/work/wrap.sh"} },
		},
		{
			name:  "an entry that adds a directory the CLI's node loads modules from",
			field: "NODE_PATH",
			apply: func(c *claudecode.Config) { c.Env = []string{"NODE_PATH=/srv/audit/work/node_modules"} },
		},
		{
			name:  "an entry that replaces the certificates the CLI's node trusts",
			field: "SSL_CERT_FILE",
			apply: func(c *claudecode.Config) { c.Env = []string{"SSL_CERT_FILE=/srv/audit/work/ca.pem"} },
		},
		{
			name:  "an entry that replaces the directory of the certificates the CLI's node trusts",
			field: "SSL_CERT_DIR",
			apply: func(c *claudecode.Config) { c.Env = []string{"SSL_CERT_DIR=/srv/audit/work/certs"} },
		},
		{
			name:  "an entry of a name outside the allow list",
			field: "SHIFT2EU_TEST_CANARY",
			apply: func(c *claudecode.Config) { c.Env = []string{"SHIFT2EU_TEST_CANARY=an-invented-value"} },
		},
	}

	// The config below is the one every row starts from, and the two fields a
	// row clears are its own (the token and the timeout).
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := claudecode.Config{Path: missingCLI, Token: subscriptionToken, Model: auditModel, Timeout: time.Minute}
			tc.apply(&cfg)

			text, err := claudecode.New(cfg).Ask(t.Context(), claudecode.Prompt{System: "s", User: "u"})
			if !errors.Is(err, claudecode.ErrModel) {
				t.Fatalf("Ask = %q, %v; want an error wrapping ErrModel", text, err)
			}

			if strings.Contains(err.Error(), "start the Claude Code CLI") {
				t.Errorf("Ask = %v; the config is refused before the CLI starts, not by its failure to start", err)
			}

			if !strings.Contains(err.Error(), tc.field) {
				t.Errorf("Ask = %v, want the error to name the %s it refused", err, tc.field)
			}

			// No refusal quotes the token (issue #277, round M, audit 2): the
			// field's own name is all the error says of it, so a config the
			// launcher refuses puts no credential of the caller's in a log.
			if strings.Contains(err.Error(), subscriptionToken) {
				t.Errorf("Ask = %v, want the error to hold none of the token: the refusal names the field, never the value the CLI's environment holds (CLAUDE.md rule 3, issue #277)", err)
			}
		})
	}
}
