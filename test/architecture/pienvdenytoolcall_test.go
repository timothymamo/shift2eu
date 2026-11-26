// Runs pi's env-printing refusal against tool calls instead of reading it (CLAUDE.md
// rule 3, ADR 0044, issue #662's second review, finding S1).
//
// The static pins in pienvdeny_test.go hold the extension's text and its pattern
// list as data, and they cannot say whether a call is refused: a check for the
// name of a file is satisfied by a comment that names it, and a branch that reads
// the wrong field refuses nothing. This file therefore loads the extension as pi
// does and hands its `tool_call` handler the calls below, through the same node
// the load check uses (pienvdenyload_test.go).
//
// The calls are the ones round 3's review left open for the App's token file (finding
// S1), which `make agents-token` writes to ~/.config/shift2eu/agents-app-token in the
// VM and the gh wrapper reads (ADR 0031, docs/dev-environment.md): every agent pushes
// with that one GitHub credential, so a file tool pointed at it, at the folder that
// holds it, or at an ancestor a grep or find walks through hands the token over,
// exactly as a read of the deck's daemon.env does. The repository's own
// dev/agents-app-token.sh mints the file and holds no credential, so reading it
// stays allowed.
//
// A search *pattern* is not a way in, and refusing one stops real work (round 3's
// review, finding S1): `grep` with a pattern names text to look for in the tree it is
// pointed at, never a file to open, and `find` with a pattern names the names the walk
// under its path reports. From the repository, both read the repository's own
// dev/agents-app-token.sh and nothing under the home directory, so a pattern naming
// the App's files is allowed there; a `glob` still selects files wherever the walk
// goes, and a path that is the folder, under it, or an ancestor of it is still
// refused.
//
// The extension reads the home directory with node's homedir(), which is the HOME
// this test hands node: the fake home is an empty directory of this test's own, so
// nothing here opens a real token file (CLAUDE.md rule 3). node's own environment is
// built here name by name, never inherited.
package architecture_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// piToolCallScript drives the extension's `tool_call` handler with the calls of the
// document at process.argv[2], and writes what the handler decided, one entry per
// call, as JSON. The extension is imported as pi imports it, and its default export
// is called with a stand-in for pi that keeps the handlers it registers.
//
// Nothing here decides anything: a call the handler allows is reported as blocked
// false, a refusal as blocked true with its reason, and a handler that throws is
// reported with what it threw, since a guard that throws on a call refuses that call
// and a guard that silently does nothing is the failure these cases exist for.
const piToolCallScript = `
const { pathToFileURL } = await import("node:url");
const { readFileSync } = await import("node:fs");

const doc = JSON.parse(readFileSync(process.argv[2], "utf8"));
const extension = await import(pathToFileURL(process.argv[1]).href);
if (typeof extension.default !== "function") {
	console.error("the extension exports no function as its default export");
	process.exit(1);
}

const handlers = new Map();
extension.default({ on(name, handler) { handlers.set(name, handler); } });

const decide = handlers.get("tool_call");
if (typeof decide !== "function") {
	console.error("the extension hooks no tool_call handler");
	process.exit(1);
}

const decisions = [];
for (const call of doc.calls) {
	let decision;
	try {
		decision = await decide({ toolName: call.toolName, input: call.input }, { cwd: doc.cwd });
	} catch (err) {
		decisions.push({ blocked: false, thrown: String((err && err.message) || err) });
		continue;
	}
	decisions.push({
		blocked: Boolean(decision && decision.block),
		reason: decision && decision.reason ? String(decision.reason) : "",
		thrown: "",
	});
}

process.stdout.write(JSON.stringify(decisions));
`

// The App's file, as `make agents-token` leaves it under the VM user's home: the folder and the file name the
// refusal has to know (ADR 0031, docs/dev-environment.md). They are spelled here rather than built from the file
// path, because the extension compares the paths pi is given, and a tilde path carries these separators whichever
// platform the tests run on.
const (
	shift2euConfigFolder = ".config/shift2eu"
	shift2euAppFile      = "agents-app-token"

	// appTokenFileStem is the prefix both files of that folder share, as a search pattern
	// writes it to find either of them.
	appTokenFileStem = "agents-app"

	// appTokenFolderPattern is the folder as a pattern names it: the escaped dot a regular
	// expression needs. A pattern is text to look for, so this is not a path to refuse.
	appTokenFolderPattern = `\.config/shift2eu`
)

// piToolCall is one call handed to the extension: the tool's name and the input pi
// passes it, field by field, exactly as the tool's own schema takes it.
type piToolCall struct {
	ToolName string            `json:"toolName"`
	Input    map[string]string `json:"input"`
}

// piToolCallDecision is what the extension answered for one call: whether it blocked
// it, the reason it gave, and what its handler threw, if anything.
type piToolCallDecision struct {
	Blocked bool   `json:"blocked"`
	Reason  string `json:"reason"`
	Thrown  string `json:"thrown"`
}

// TestPiEnvDenyExtensionRefusesTheAppTokenFolderOfFileTools pins the App's token
// file against pi's file tools: the file itself, the folder under the home directory
// that holds it, anything else in that folder, a grep or find pointed at the folder or
// at an ancestor of it, and a glob that names the folder's files are all refused; a
// search pattern that names them is not, and neither is the repository's own script of
// that name, since it mints the token and holds none (issue #662, finding S1).
func TestPiEnvDenyExtensionRefusesTheAppTokenFolderOfFileTools(t *testing.T) {
	t.Parallel()

	nodePath := piToolCallNode(t)

	root := repositoryRoot(t)
	home := t.TempDir()

	tokenFile := filepath.Join(home, filepath.FromSlash(shift2euConfigFolder), shift2euAppFile)

	cases := []struct {
		name string
		call piToolCall
		// refused is whether the extension has to block the call.
		refused bool
	}{
		// The token file, by the tilde pi resolves and by the absolute path node
		// resolves it to: the extension resolves a file tool's path the way pi does,
		// so the two spellings are one file.
		{
			name:    "read the App's token file by the tilde path",
			call:    piToolCall{ToolName: "read", Input: map[string]string{"path": "~/" + shift2euConfigFolder + "/" + shift2euAppFile}},
			refused: true,
		},
		{
			name:    "read the App's token file by its absolute path",
			call:    piToolCall{ToolName: "read", Input: map[string]string{"path": tokenFile}},
			refused: true,
		},
		{
			name:    "read the App's token file by a file:// URL",
			call:    piToolCall{ToolName: "read", Input: map[string]string{"path": "file://" + tokenFile}},
			refused: true,
		},
		// The folder itself, which a read of it or a grep of it walks.
		{
			name:    "read the folder that holds the App's token",
			call:    piToolCall{ToolName: "read", Input: map[string]string{"path": "~/" + shift2euConfigFolder}},
			refused: true,
		},
		{
			name:    "read the folder that holds the App's token, with a trailing slash",
			call:    piToolCall{ToolName: "read", Input: map[string]string{"path": "~/" + shift2euConfigFolder + "/"}},
			refused: true,
		},
		{
			// The file the refresh writes beside the token, which leads to the token as
			// surely as the token itself: the folder is refused whole.
			name:    "read another file of that folder",
			call:    piToolCall{ToolName: "read", Input: map[string]string{"path": filepath.Join(home, filepath.FromSlash(shift2euConfigFolder), "agents-app-identity")}},
			refused: true,
		},
		// grep and find read the contents of the tree they are pointed at, so the
		// folder, an ancestor of it and a pattern that names it are each a way in.
		{
			name:    "grep the folder that holds the App's token",
			call:    piToolCall{ToolName: "grep", Input: map[string]string{"path": "~/" + shift2euConfigFolder, "pattern": "token"}},
			refused: true,
		},
		{
			name:    "grep ~/.config, an ancestor of that folder",
			call:    piToolCall{ToolName: "grep", Input: map[string]string{"path": "~/.config", "pattern": "token"}},
			refused: true,
		},
		{
			name:    "grep the home directory, an ancestor of that folder",
			call:    piToolCall{ToolName: "grep", Input: map[string]string{"path": "~", "pattern": "token"}},
			refused: true,
		},
		{
			// A pattern is text the walk looks for, not a file it opens, and the
			// repository holds only dev/agents-app-token.sh under that name: refusing
			// the search would stop an agent reading what the script does.
			name: "grep of the repository with a pattern naming the token file",
			call: piToolCall{ToolName: "grep", Input: map[string]string{"path": root, "pattern": shift2euAppFile}},
		},
		{
			name: "grep of the repository, with no path, with a pattern naming the token file",
			call: piToolCall{ToolName: "grep", Input: map[string]string{"pattern": shift2euAppFile}},
		},
		{
			name: "grep of the repository with the App's file-name stem as its pattern",
			call: piToolCall{ToolName: "grep", Input: map[string]string{"path": root, "pattern": appTokenFileStem}},
		},
		{
			name: "grep of the repository with the App's folder as its pattern",
			call: piToolCall{ToolName: "grep", Input: map[string]string{"path": root, "pattern": appTokenFolderPattern}},
		},
		{
			name:    "grep of the repository with a glob naming the token file",
			call:    piToolCall{ToolName: "grep", Input: map[string]string{"path": root, "glob": "agents-app-t*"}},
			refused: true,
		},
		// A find's pattern is a name the walk reports, not a file it opens, and a walk of
		// the repository reports the script that mints the token and nothing else.
		{
			name: "find of the repository with a pattern naming the token file",
			call: piToolCall{ToolName: "find", Input: map[string]string{"path": root, "pattern": shift2euAppFile}},
		},
		{
			name: "find of the repository, with no path, with a pattern naming the token file",
			call: piToolCall{ToolName: "find", Input: map[string]string{"pattern": shift2euAppFile}},
		},
		{
			// The command an agent runs to find the script: the pattern names it, the walk
			// stays in the repository, and no credential is read.
			name: "find of the repository with the token-minting script as its pattern",
			call: piToolCall{ToolName: "find", Input: map[string]string{"path": root, "pattern": shift2euAppFile + ".sh"}},
		},
		// A walk of the home directory reaches the folder itself, whatever its pattern
		// says, so the pattern changes nothing about the refusal there.
		{
			name:    "find of the home directory, an ancestor of that folder",
			call:    piToolCall{ToolName: "find", Input: map[string]string{"path": "~", "pattern": "*.token"}},
			refused: true,
		},
		{
			name:    "find of the home directory with a pattern naming the token file",
			call:    piToolCall{ToolName: "find", Input: map[string]string{"path": "~", "pattern": shift2euAppFile}},
			refused: true,
		},
		// What the refusal must leave alone: the script that mints the token holds no
		// credential, and the refusal of the token file must not reach it, nor the work
		// of reading the repository.
		{
			name: "read the script that mints the App's token",
			call: piToolCall{ToolName: "read", Input: map[string]string{"path": "dev/agents-app-token.sh"}},
		},
		{
			name: "read the development environment's documentation",
			call: piToolCall{ToolName: "read", Input: map[string]string{"path": "docs/dev-environment.md"}},
		},
		{
			name: "grep the repository",
			call: piToolCall{ToolName: "grep", Input: map[string]string{"path": ".", "pattern": "shift2eu"}},
		},
		{
			name: "read a file of the repository that is none of the above",
			call: piToolCall{ToolName: "read", Input: map[string]string{"path": "CLAUDE.md"}},
		},
	}

	calls := make([]piToolCall, 0, len(cases))

	for _, tc := range cases {
		calls = append(calls, tc.call)
	}

	decisions := piRunToolCalls(t, nodePath, filepath.Join(root, filepath.FromSlash(piEnvDenyExtension)), root, home, calls)

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			decision := decisions[i]

			if decision.Thrown != "" {
				t.Errorf("the extension's tool_call handler threw on %s of %q: %s. A handler that throws on a call refuses it for a reason nobody can act on, and the guard it stands for has not been shown to work (issue #662, CLAUDE.md rule 3)",
					tc.call.ToolName, tc.call.Input, decision.Thrown)

				return
			}

			if tc.refused {
				if !decision.Blocked {
					t.Errorf("the extension allows %s of %v, which reaches the App's token file %s under the home directory: that one GitHub credential is what every agent pushes with, so a file tool pointed at the token, at the folder that holds it, at an ancestor a walk passes through or at a glob naming it has to be refused as a read of the deck's %s is (issue #662, ADR 0031, CLAUDE.md rule 3)",
						tc.call.ToolName, tc.call.Input, "~/.config/shift2eu/agents-app-token", "daemon.env")
				}

				return
			}

			if decision.Blocked {
				t.Errorf("the extension refuses %s of %v, which reads no credential: a walk of the repository finds the script that mints the token and holds none, a pattern names text to look for rather than a file to open, and a refusal here stops the agents' work (issue #662, CLAUDE.md rule 3); it said: %s",
					tc.call.ToolName, tc.call.Input, decision.Reason)
			}
		})
	}
}

// piToolCallNode returns the node the tool-call cases run on, and skips with a
// reason where the machine has none that strips TypeScript types: the extension is a
// .ts file, and a node that cannot import it would fail every case for a reason that
// is not the extension's. SHIFT2EU_REQUIRE_NODE=1 turns the skip into a failure, as
// it does for the load cases.
func piToolCallNode(t *testing.T) string {
	t.Helper()

	nodePath, err := exec.LookPath("node")

	state := piNodeState{
		found:    err == nil,
		pinned:   piPinnedNodeVersion(t),
		required: os.Getenv("SHIFT2EU_REQUIRE_NODE") == "1",
	}

	if state.found {
		state.version, state.strips = piProbeNode(t, nodePath)
	}

	switch verdict, reason := piNodeDecision(state); verdict {
	case piNodeRun:
		return nodePath
	case piNodeSkip:
		t.Skip(reason)
	case piNodeFail:
		t.Fatal(reason)
	}

	// Unreachable: piNodeDecision answers one of the three verdicts above.
	return ""
}

// piRunToolCalls hands calls to the extension's tool_call handler through node, and
// returns what it decided for each, in the order it was given them. The calls travel
// as a file that node reads, so that no path, pattern or glob of a case is part of a
// program's text, and node's environment is built here: the home directory the
// extension resolves `~` against, a PATH of its own rather than the machine's, and a
// temporary directory of the test's own (CLAUDE.md rule 3).
func piRunToolCalls(t *testing.T, nodePath, extensionPath, cwd, home string, calls []piToolCall) []piToolCallDecision {
	t.Helper()

	doc, err := json.Marshal(struct {
		Cwd   string       `json:"cwd"`
		Calls []piToolCall `json:"calls"`
	}{Cwd: cwd, Calls: calls})
	if err != nil {
		t.Fatalf("encode the calls: %v", err)
	}

	callsPath := filepath.Join(t.TempDir(), "calls.json")

	if err := os.WriteFile(callsPath, doc, 0o600); err != nil {
		t.Fatalf("write the calls: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, nodePath, "--input-type=module", "-e", piToolCallScript, extensionPath, callsPath)
	cmd.Dir = cwd
	cmd.Env = []string{
		"HOME=" + home,
		"PATH=" + filepath.Dir(nodePath) + ":/usr/bin:/bin",
		"TMPDIR=" + t.TempDir(),
	}

	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("node did not run the tool calls before the timeout: %v\n%s", ctx.Err(), out)
	}

	if err != nil {
		t.Fatalf("node could not run %s's tool_call handler, so no call here could be decided: %v\n%s\nPi logs a load failure and runs without the refusal, so this is the failure these cases exist for (ADR 0044, issue #662)",
			piEnvDenyExtension, err, out)
	}

	var decisions []piToolCallDecision
	if err := json.Unmarshal(out, &decisions); err != nil {
		t.Fatalf("node answered %q, which is no JSON array of decisions: %v", out, err)
	}

	if len(decisions) != len(calls) {
		t.Fatalf("node answered %d decisions for %d calls, so no case here can trust its answer: %q", len(decisions), len(calls), out)
	}

	return decisions
}
