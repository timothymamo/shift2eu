// Pins .github/security-audit/claude-settings.json, the only settings file the
// audit's Claude Code CLI loads (ADR 0040's model-job amendment, issue #277,
// PRD #175 milestone 2). The ADR names this test: dropping or widening any
// entry below fails make test.
//
// The file is the audit's permission model: --setting-sources "" loads no user,
// project or local settings file, and --settings names this one, so a later
// change to .claude/settings.json cannot widen the audit. It is read here as a
// closed document: every key and every value it holds is one the ADR lists, a
// key it lists is present, and no key is written twice, which JSON would let a
// later one override silently.
package architecture_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// claudeSettingsPath is the audit's settings file, relative to the module root:
// it is read from the checkout copy at
// /srv/audit/checkout/.github/security-audit/claude-settings.json.
const claudeSettingsPath = ".github/security-audit/claude-settings.json"

// claudeSettings is the document ADR 0040's model-job amendment fixes, key for key and
// value for value, in the order the ADR writes them. Every object here is
// compared whole, so an entry the ADR does not list fails the test as surely as
// a missing one.
//
// The allow list's paths are Claude Code's rule syntax, where // starts an
// absolute path: / alone would be relative to the settings file and ~ is the
// launcher's fresh HOME. A Read rule covers Read, Grep and Glob, an Edit rule
// covers Edit and Write, and no Grep, Glob or Write rule is written because a
// settings file the CLI cannot validate is ignored silently in -p mode; the
// preflight's probes hold the rules to each tool on every run.
var claudeSettings = map[string]any{
	"permissions": map[string]any{
		// Inert under CLAUDE_CODE_SUBPROCESS_ENV_SCRUB=1, which makes Claude Code
		// 2.1.332 run the default mode whatever this file says: the audit's launch
		// asks for default explicitly and the launcher checks the mode the session
		// reports (ADR 0040, issue #592). The entry stays the stricter value for a
		// CLI that would honour it.
		"defaultMode":                  "dontAsk",
		"disableBypassPermissionsMode": "disable",
		"allow": []any{
			"Read(//srv/audit/checkout/**)",
			"Read(//srv/audit/checks/**)",
			"Read(//srv/audit/work/**)",
			"Edit(//srv/audit/work/**)",
			"Bash(/srv/audit/bin/audit-exec:*)",
		},
		"deny": []any{
			"Read(//proc)",
			"Read(//proc/**)",
			"Read(//sys)",
			"Read(//sys/**)",
			"Read(//etc)",
			"Read(//etc/**)",
			"Read(//home)",
			"Read(//home/**)",
			"Read(//home/runner/**)",
			"Read(//srv/audit/state)",
			"Read(//srv/audit/state/**)",
			"Edit(//srv/audit/state/**)",
			"Bash(git push:*)",
			"Bash(git commit:*)",
			"Bash(git tag:*)",
			"Bash(git remote:*)",
			"Bash(gh:*)",
			"Bash(curl:*)",
			"Bash(wget:*)",
			"Bash(nc:*)",
			"Bash(ssh:*)",
			"Bash(docker:*)",
			"Bash(sudo:*)",
			"Bash(env:*)",
			"Bash(printenv:*)",
			"Bash(set:*)",
			"Bash(*/proc/*/environ*)",
		},
	},
	"sandbox": map[string]any{
		"enabled":                   true,
		"failIfUnavailable":         true,
		"autoAllowBashIfSandboxed":  false,
		"allowUnsandboxedCommands":  false,
		"enableWeakerNestedSandbox": false,
		"network": map[string]any{
			"allowedDomains":    []any{},
			"allowUnixSockets":  []any{},
			"allowLocalBinding": false,
		},
	},
	"disableAllHooks":            true,
	"enableAllProjectMcpServers": false,
}

// TestClaudeSettingsFileIsTheADRsOnly pins the audit's settings file whole: it
// is JSON, it holds no key twice, and its document is exactly the one the ADR
// fixes. A key the ADR does not list (hooks, mcpServers, env, apiKeyHelper,
// awsCredentialExport, otelHeadersHelper, sandbox.filesystem,
// sandbox.excludedCommands, a second permission mode) is a widening this test
// exists to refuse, as is a key of the ADR's that went missing.
func TestClaudeSettingsFileIsTheADRsOnly(t *testing.T) {
	t.Parallel()

	data := readRepositoryFile(t, claudeSettingsPath)

	got, err := decodeJSONDocument(data)
	if err != nil {
		t.Fatalf("%s: %v", claudeSettingsPath, err)
	}

	if diff := cmp.Diff(claudeSettings, got); diff != "" {
		t.Errorf("%s: the settings differ from the ones ADR 0040's model-job amendment (\"How Claude Code runs in the model job\", The settings file) fixes (-want +got):\n%s", claudeSettingsPath, diff)
	}
}

// claudeProjectSettingsPath is the project's own settings file, the layer every
// Claude Code role loads beside the rule in CLAUDE.md (issue #540).
const claudeProjectSettingsPath = ".claude/settings.json"

// claudeProjectEnvDeny are the rules the project's settings file must refuse.
// They are matched by presence, not as a whole set: the file has other rules,
// and a rule may be written again beside them. `Bash(env)` is bare `env` alone;
// `Bash(env *)` matches every `env` with an argument, `env VAR=value cmd`
// included, because Claude Code's prefix rules cannot see an argument as
// harmless: the roles refuse `env` with arguments altogether (ADR 0044).
// `Bash(set)`, `Bash(declare)` and `Bash(typeset)` are the bare builtins, and the
// options that list variables and exports are refused by their own rules
// (`export -p*`, `declare -p*`/`-x*`, `typeset -p*`/`-x*`, `compgen -v*`/`-e*`),
// as is `compgen -A` whatever list it names: `variable` and `export` print the
// environment, and a rule that enumerates them one by one would miss one.
// `/proc/*/environ` is refused as a command's argument and as a file a tool
// reads, because either prints every value in the process; `ls /proc` and
// `cat /proc/*/environ` reach it, so the directory is refused whole. `ps e*` is
// `ps` with the environment column, `make -p`/`--print-data-base` print the
// environment of every target, `Bash(*daemon.env*)` the deck's own file of keys,
// `Bash(*.config/dot-agent-deck*)` the directory that holds it and everything
// else the deck keeps there, and a `$NAME` that expands a key (`*$*API_KEY*`,
// `*$*TOKEN*`) is refused with it; `&>` redirects both streams and `[0-9]>`
// redirects one of them by number, and Claude Code may not strip either before
// matching the exact rule, so each printer names those forms too.
// The Read rules start with `//`: a single leading `/` would be relative to
// the settings file's project root and never match the real path.
var claudeProjectEnvDeny = []string{
	"Bash(env)",
	"Bash(env *)",
	"Bash(/usr/bin/env*)",
	"Bash(printenv)",
	"Bash(printenv *)",
	"Bash(set)",
	// A redirect on a bare printer: Claude Code may not strip the redirect
	// before matching the exact `Bash(set)` and `Bash(export)` rules, so the
	// numbered and unnumbered forms are named here too.
	"Bash(set >*)",
	"Bash(set [0-9]>*)",
	"Bash(set &>*)",
	"Bash(export)",
	"Bash(export >*)",
	"Bash(export [0-9]>*)",
	"Bash(export &>*)",
	"Bash(export -p*)",
	"Bash(declare)",
	"Bash(declare >*)",
	"Bash(declare [0-9]>*)",
	"Bash(declare &>*)",
	"Bash(declare -p*)",
	"Bash(declare -x*)",
	"Bash(typeset)",
	"Bash(typeset >*)",
	"Bash(typeset [0-9]>*)",
	"Bash(typeset &>*)",
	"Bash(typeset -p*)",
	"Bash(typeset -x*)",
	"Bash(compgen -v*)",
	"Bash(compgen -e*)",
	"Bash(compgen -A*)",
	"Bash(compgen *-A*)",
	"Bash(*/proc/*/environ*)",
	"Bash(make -p*)",
	"Bash(make --print-data-base*)",
	"Bash(ps e*)",
	"Bash(*daemon.env*)",
	// The deck's whole key directory, not only the one file this project knows the
	// name of: `grep -r .` over it prints every value in it.
	"Bash(*.config/dot-agent-deck*)",
	"Bash(*$*API_KEY*)",
	"Bash(*$*TOKEN*)",
	// `gh` prints its own token, and it is a tool the agents install and would
	// reach for when the shell's printers are refused. `gh auth status` alone is
	// allowed (it names the account, not the token), so its `-t` and
	// `--show-token` forms are refused on their own.
	"Bash(gh auth token*)",
	"Bash(gh auth status*-t*)",
	"Bash(gh auth status*--show-token*)",
	// A credential helper prints the token it fills in, as surely as `gh auth
	// token` does: `git credential fill` writes the password to stdout, and `gh`
	// answers the same protocol itself. The wildcards cover the helper whatever
	// feeds it (a pipeline, a redirect, a plain invocation), while the git
	// commands that only read configuration or push a branch stay allowed.
	"Bash(*git credential fill*)",
	"Bash(*gh auth git-credential*)",
	"Read(//proc)",
	"Read(//proc/**)",
	"Read(//proc/**/environ)",
	"Read(~/.config/dot-agent-deck/**)",
}

// claudeProjectEnvDenyDead is the rule the settings file must not hold: a path
// that starts with a single `/` is relative to the settings file's project root
// in Claude Code, so `Read(/proc/*/environ)` never matches the real /proc and
// reads as a refusal it is not. The `//` rule beside it is the one that holds.
const claudeProjectEnvDenyDead = "Read(/proc/*/environ)"

// TestProjectClaudeSettingsDenyPrintingTheEnvironment pins the second layer
// under CLAUDE.md rule 3 for the Claude Code roles (issue #540): each rule above
// is in .claude/settings.json's permissions.deny, none of them is also in
// permissions.allow, where a later reader would take the command for allowed,
// and the project-relative `/proc` rule, which refuses nothing, is gone.
// The file is read as a closed document first, so a key written twice cannot
// hide a second, wider allow list from this test.
func TestProjectClaudeSettingsDenyPrintingTheEnvironment(t *testing.T) {
	t.Parallel()

	data := readRepositoryFile(t, claudeProjectSettingsPath)

	doc, err := decodeJSONDocument(data)
	if err != nil {
		t.Fatalf("%s: %v", claudeProjectSettingsPath, err)
	}

	allow, deny := projectPermissionRules(t, doc)

	for _, rule := range claudeProjectEnvDeny {
		if _, ok := deny[rule]; !ok {
			t.Errorf("%s: permissions.deny does not hold %q, so the role runs the command CLAUDE.md rule 3 forbids (issue #540)", claudeProjectSettingsPath, rule)
		}

		if _, ok := allow[rule]; ok {
			t.Errorf("%s: permissions.allow holds %q, which permissions.deny refuses and must not be handed back (issue #540)", claudeProjectSettingsPath, rule)
		}
	}

	if _, ok := deny[claudeProjectEnvDenyDead]; ok {
		t.Errorf("%s: permissions.deny holds %q, whose single leading `/` makes it project-relative: it matches no real /proc and is dead (issue #540); write the rule with `//`", claudeProjectSettingsPath, claudeProjectEnvDenyDead)
	}
}

// projectPermissionRules returns the rule strings of the document's
// permissions.allow and permissions.deny lists as sets, and fails the test when
// the document holds no permissions object or one of the two lists is missing or
// no list of strings: a settings file Claude Code cannot read refuses nothing.
func projectPermissionRules(t *testing.T, doc any) (allow, deny map[string]struct{}) {
	t.Helper()

	root, ok := doc.(map[string]any)
	if !ok {
		t.Fatalf("%s: the document is no JSON object", claudeProjectSettingsPath)
	}

	permissions, ok := root["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("%s: no permissions object", claudeProjectSettingsPath)
	}

	return projectRules(t, permissions, "allow"), projectRules(t, permissions, "deny")
}

// projectRules returns permissions[key] as a set of rule strings.
func projectRules(t *testing.T, permissions map[string]any, key string) map[string]struct{} {
	t.Helper()

	value, ok := permissions[key]
	if !ok {
		t.Fatalf("%s: permissions holds no %s list", claudeProjectSettingsPath, key)
	}

	list, ok := value.([]any)
	if !ok {
		t.Fatalf("%s: permissions.%s is no list", claudeProjectSettingsPath, key)
	}

	rules := make(map[string]struct{}, len(list))

	for _, entry := range list {
		rule, ok := entry.(string)
		if !ok {
			t.Fatalf("%s: permissions.%s holds a value that is no rule: %v", claudeProjectSettingsPath, key, entry)
		}

		rules[rule] = struct{}{}
	}

	return rules
}

// TestClaudeSettingsFileRefusesADuplicateKey pins the reader the test above
// reads the file with: encoding/json keeps the last of two equal keys silently,
// so a file could hold a second permissions object or a second allow list that
// a reader of it never sees but a later one does.
func TestClaudeSettingsFileRefusesADuplicateKey(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		doc  string
		want string
	}{
		{
			name: "a key twice at the top level",
			doc:  `{"disableAllHooks":true,"disableAllHooks":false}`,
			want: "disableAllHooks",
		},
		{
			name: "a key twice in a nested object",
			doc:  `{"sandbox":{"enabled":true,"enabled":false}}`,
			want: "sandbox.enabled",
		},
		{
			name: "a key twice in an object inside an array",
			doc:  `{"permissions":[{"defaultMode":"dontAsk","defaultMode":"bypassPermissions"}]}`,
			want: "permissions[0].defaultMode",
		},
		{
			name: "the same key in two objects is no duplicate",
			doc:  `{"a":{"x":1},"b":{"x":2}}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := decodeJSONDocument([]byte(tc.doc))

			if tc.want == "" {
				if err != nil {
					t.Fatalf("decodeJSONDocument(%s) error = %v, want none", tc.doc, err)
				}

				return
			}

			if err == nil {
				t.Fatalf("decodeJSONDocument(%s) = nil, want an error naming the repeated key %s", tc.doc, tc.want)
			}

			if want := tc.want; !bytes.Contains([]byte(err.Error()), []byte(want)) {
				t.Errorf("decodeJSONDocument(%s) error = %q, want it to name %q", tc.doc, err, want)
			}
		})
	}
}

// decodeJSONDocument reads data as one JSON document into map[string]any,
// []any, string, bool, json.Number and nil, and refuses a document that is no
// JSON, that holds more than one value, or that writes a key of one object
// twice: encoding/json keeps the last of two equal keys silently, so a repeated
// key is an error here. The duplicate is named by its path, so a failure says
// where to look.
func decodeJSONDocument(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()

	value, err := decodeJSONValue(dec, "")
	if err != nil {
		return nil, err
	}

	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("the document holds more than one value")
	}

	return value, nil
}

// decodeJSONValue reads the value the decoder is at, path being the object
// path it sits under for a duplicate key's message.
func decodeJSONValue(dec *json.Decoder, path string) (any, error) {
	token, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("read the document: %w", err)
	}

	delim, ok := token.(json.Delim)
	if !ok {
		return token, nil
	}

	switch delim {
	case '{':
		object := map[string]any{}

		for dec.More() {
			keyToken, err := dec.Token()
			if err != nil {
				return nil, fmt.Errorf("read a key of %s: %w", jsonPath(path), err)
			}

			key, ok := keyToken.(string)
			if !ok {
				return nil, fmt.Errorf("%s: a key is no string", jsonPath(path))
			}

			if _, twice := object[key]; twice {
				return nil, fmt.Errorf("%s: the key is written twice", jsonPath(path+"."+key))
			}

			value, err := decodeJSONValue(dec, path+"."+key)
			if err != nil {
				return nil, err
			}

			object[key] = value
		}

		if _, err := dec.Token(); err != nil {
			return nil, fmt.Errorf("close %s: %w", jsonPath(path), err)
		}

		return object, nil
	case '[':
		// An empty JSON array decodes to an empty slice, not a nil one: the
		// wanted document writes its empty lists as []any{}, and cmp.Diff tells
		// nil from empty.
		array := []any{}

		for dec.More() {
			index := len(array)

			value, err := decodeJSONValue(dec, fmt.Sprintf("%s[%d]", path, index))
			if err != nil {
				return nil, err
			}

			array = append(array, value)
		}

		if _, err := dec.Token(); err != nil {
			return nil, fmt.Errorf("close %s: %w", jsonPath(path), err)
		}

		return array, nil
	default:
		return nil, fmt.Errorf("%s: the document holds an unexpected %q", jsonPath(path), delim)
	}
}

// jsonPath names path for an error, the document itself being "$".
func jsonPath(path string) string {
	if path == "" {
		return "$"
	}

	if path[0] == '.' {
		return "$" + path
	}

	return path
}
