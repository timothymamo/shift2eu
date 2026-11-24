// Runs pi's refusal of the commands that print the environment (CLAUDE.md
// rule 3, ADR 0044) instead of reading it (issue #548).
//
// The static pins in pienvdeny_test.go hold the extension's text and its pattern
// list as data. None of them can see the one failure that loses the refusal
// whole: pi logs an extension that throws while it loads and carries on without
// it, so a top-level statement that throws (an invalid regular expression, a
// call whose argument a later edit got wrong) fails open, and outside a TUI pane
// nobody sees the log. `make check` therefore has to load the extension and fail
// when it cannot.
//
// pi loads extensions with jiti, the TypeScript loader it ships. This check
// loads the file with Node's own type stripping instead, a stricter stand-in for
// that loader: a top level that throws fails both, and syntax that needs a
// transform (an `enum`, a `namespace`, a parameter property) fails this check
// although pi would accept it, so the extension keeps to erasable syntax. Type
// stripping is on by default from Node 22.18 / 23.6, and only such a Node can
// load the file here: one that cannot is skipped or failed as the environment
// asks, never reported as a broken extension.
//
// Loading is all this needs: importing the module evaluates its top level, which
// does nothing (the list is read in the factory, when pi calls it), so no refused
// command runs here and nothing is passed to a shell. Type stripping erases the
// `import type` of pi's package, so the check needs no loader and no package of
// its own: only node, which the VM installs for pi (dev/lima.yaml, nodeVersion).
//
// The test runs whatever `node` the PATH holds. Where there is none (a Mac with
// no Node, a runner that has not set one up) or where it is too old to strip
// types by default, it skips with a message naming the Node dev/lima.yaml pins,
// and fails instead when SHIFT2EU_REQUIRE_NODE=1 is set, which is how CI refuses
// a silent skip everywhere (ci.yml's and release.yml's Test jobs install the
// pinned Node and set it).
package architecture_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// piNodeLoadScript is the program node runs: it imports the file it is given as
// an argument on the command line, and exits non-zero unless the module it
// imported has a function as its default export, which is what pi calls to build
// the extension.
//
// The path is an argument (process.argv[1]), never part of this text, so a path
// holding a quote, a backslash or a `$` changes nothing about what runs. A load
// that throws fails the import, and the rejection is reported with node's own
// message, so a failing case can say why it failed.
const piNodeLoadScript = `
const { pathToFileURL } = await import("node:url");
const file = process.argv[1];

try {
	const module = await import(pathToFileURL(file).href);
	if (typeof module.default !== "function") {
		console.error("default export of " + file + " is " + typeof module.default + ", not a function");
		process.exit(1);
	}
	console.log("default export is a function");
} catch (err) {
	console.error("importing " + file + " failed: " + (err && err.message));
	process.exit(1);
}
`

// piNodeProbeScript is the program node runs before the load cases: it says, in
// one line, what version it is and whether it strips TypeScript types on import
// (process.features.typescript, false or missing in a Node older than the one
// that turned it on by default). Neither answer decides anything here; node's
// own report does, and piNodeDecision reads it.
const piNodeProbeScript = `
process.stdout.write(JSON.stringify({
	version: process.version,
	strips: Boolean(process.features?.typescript),
}));
`

// The two things node says that the cases below pin, each the half of the check
// its case is about: a module loaded and exported a function, and a copy whose
// import loaded but exported no function (the load failures are pinned by node's
// own message, which says what threw).
const (
	piNodeLoaded          = "default export is a function"
	piNodeNoFunctionNamed = "not a function"
)

// TestPiEnvDenyExtensionLoads pins that the extension load succeeds, and that the
// check would notice the two ways it stops refusing: a top level that throws,
// which pi logs and survives without the refusal (ADR 0044), and a module that
// exports no function, which pi cannot call at all. The broken copies are
// written at test time into a temporary directory, so the extension itself stays
// as the static pins hold it.
func TestPiEnvDenyExtensionLoads(t *testing.T) {
	t.Parallel()

	pinned := piPinnedNodeVersion(t)

	// node is the one program this check needs, and the pinned one is in the VM
	// (dev/lima.yaml, nodeVersion). It also has to strip TypeScript types, which
	// only a recent one does: an old node would fail the import with
	// ERR_UNKNOWN_FILE_EXTENSION, and a check that reported that as a broken
	// extension would send its reader after the wrong file. piNodeDecision draws
	// the line between the two.
	nodePath, err := exec.LookPath("node")

	state := piNodeState{
		found:    err == nil,
		pinned:   pinned,
		required: os.Getenv("SHIFT2EU_REQUIRE_NODE") == "1",
	}

	if state.found {
		state.version, state.strips = piProbeNode(t, nodePath)
	}

	switch verdict, reason := piNodeDecision(state); verdict {
	case piNodeRun:
	case piNodeSkip:
		t.Skip(reason)
	case piNodeFail:
		t.Fatal(reason)
	}

	extension := readRepositoryFile(t, piEnvDenyExtension)

	// The third case loads a copy whose default export is renamed, and so has
	// none: a replacement that no longer matches would leave that copy exporting
	// the function and pin nothing.
	const defaultExport = "export default function (pi: ExtensionAPI)"

	noDefaultExport := bytes.Replace(extension, []byte(defaultExport), []byte("function piExtension(pi: ExtensionAPI)"), 1)
	if bytes.Equal(noDefaultExport, extension) {
		t.Fatalf("%s no longer holds %q, so the case that loads a copy without a default export pins nothing: give it the extension's default export as the file writes it (issue #548)", piEnvDenyExtension, defaultExport)
	}

	cases := []struct {
		name string
		// source is the text loaded as the extension. Empty loads pi's own file,
		// in place, with node's type stripping: the copies that fail are written
		// into a temporary directory, since the file itself is never broken.
		source []byte
		// wantLoaded is whether the import has to succeed with a function as its
		// default export.
		wantLoaded bool
		// want is what node has to say, so that a load which failed for another
		// reason than the one its case breaks is no pass.
		want string
	}{
		{
			name:       "pi's own extension, in place",
			wantLoaded: true,
			want:       piNodeLoaded,
		},
		{
			name: "a copy whose top level throws",
			// A regex that does not compile throws where it stands, at the top
			// level, which is a load failure pi survives without the refusal: the
			// extension builds a class from code points and reads the list with
			// `new RegExp` in the factory, so this is the shape an edit takes when
			// it gets one of them wrong.
			source: []byte(string(extension) + "\nnew RegExp(\"(\");\n"),
			want:   "Invalid regular expression",
		},
		{
			name:   "a copy whose default export is not a function",
			source: noDefaultExport,
			want:   piNodeNoFunctionNamed,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(repositoryRoot(t), filepath.FromSlash(piEnvDenyExtension))

			if tc.source != nil {
				path = filepath.Join(t.TempDir(), filepath.Base(filepath.FromSlash(piEnvDenyExtension)))

				if err := os.WriteFile(path, tc.source, 0o600); err != nil {
					t.Fatalf("write the copy of %s: %v", piEnvDenyExtension, err)
				}
			}

			out, err := piLoadExtension(t, nodePath, path)

			if tc.wantLoaded {
				if err != nil {
					t.Fatalf("node could not load %s: %v\n%s\nPi logs a load failure and runs without the refusal, so this is the failure this test exists for: the extension's top level must do nothing that can throw (ADR 0044, issue #548)", path, err, out)
				}

				if !strings.Contains(string(out), tc.want) {
					t.Errorf("loading %s said %q, want it to say %q (issue #548)", path, out, tc.want)
				}

				return
			}

			if err == nil {
				t.Fatalf("%s: node loaded %s, want the load to fail: this is the copy that no longer refuses anything, so a check that loads it is a check that cannot see the extension break (issue #548)\n%s", tc.name, path, out)
			}

			if !strings.Contains(string(out), tc.want) {
				t.Errorf("loading %s failed, but not for the reason its case pins: want node to say %q, got:\n%s", path, tc.want, out)
			}
		})
	}
}

// TestPiNodeDecisionSkipsOrFailsWhatItCannotLoad pins which node the load cases
// run on, without depending on the Node this machine has. A node that strips
// TypeScript types runs them; one that does not, and no node at all, skip with a
// message that names what to install and fail when SHIFT2EU_REQUIRE_NODE=1 says
// the check has to run here. Neither ever reaches the load cases: a wrong Node
// must look like a wrong Node, not like a broken extension (issue #548).
func TestPiNodeDecisionSkipsOrFailsWhatItCannotLoad(t *testing.T) {
	t.Parallel()

	const pinned = "26.13.1"

	cases := []struct {
		name  string
		state piNodeState
		want  piNodeVerdict
		// says is what the reason names, so a skip or a failure in a log says
		// what is wrong and what to install.
		says []string
	}{
		{
			// version is what piProbeNode reports, and process.version carries a
			// leading v; pinned is what piPinnedNodeVersion returns, without one.
			name:  "the pinned node, which strips types",
			state: piNodeState{found: true, version: "v" + pinned, strips: true, pinned: pinned, required: true},
			want:  piNodeRun,
		},
		{
			name:  "a node too old to strip types, where a skip is allowed",
			state: piNodeState{found: true, version: "v20.19.0", pinned: pinned},
			want:  piNodeSkip,
			says:  []string{"v20.19.0", pinned, "22.18 / 23.6", "#548"},
		},
		{
			name:  "a node too old to strip types, where the check has to run",
			state: piNodeState{found: true, version: "v20.19.0", pinned: pinned, required: true},
			want:  piNodeFail,
			says:  []string{"v20.19.0", pinned, "22.18 / 23.6", "SHIFT2EU_REQUIRE_NODE=1", "#548"},
		},
		{
			name:  "no node at all, where a skip is allowed",
			state: piNodeState{pinned: pinned},
			want:  piNodeSkip,
			says:  []string{"PATH", pinned, "SHIFT2EU_REQUIRE_NODE=1", "#548"},
		},
		{
			name:  "no node at all, where the check has to run",
			state: piNodeState{pinned: pinned, required: true},
			want:  piNodeFail,
			says:  []string{"PATH", pinned, "SHIFT2EU_REQUIRE_NODE=1", "#548"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			verdict, reason := piNodeDecision(tc.state)

			if verdict != tc.want {
				t.Fatalf("piNodeDecision(%+v) = %v (%q), want %v (issue #548)", tc.state, verdict, reason, tc.want)
			}

			if tc.want == piNodeRun {
				if reason != "" {
					t.Errorf("piNodeDecision(%+v) runs the cases, but says %q: a reason is for the run that does not happen", tc.state, reason)
				}

				return
			}

			for _, want := range tc.says {
				if !strings.Contains(reason, want) {
					t.Errorf("piNodeDecision(%+v) says %q, want it to name %q (issue #548)", tc.state, reason, want)
				}
			}
		})
	}
}

// TestPiNodeEnvHoldsOnlyTheAllowList pins that node's environment is built from
// the allow list, by name, and holds nothing else of the inherited one (the
// audit's finding on issue #548): in a pane the inherited environment holds the
// agents' API keys (CLAUDE.md, rule 3), and the load cases need none of it. The
// fake lookup stands in for the process's environment, which a test here never
// sets (test/lint's tests-no-process-state).
func TestPiNodeEnvHoldsOnlyTheAllowList(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		inherited map[string]string
		want      []string
	}{
		{
			name: "every allow-listed variable, and others beside them",
			inherited: map[string]string{
				"PATH":                "/usr/local/node/bin:/usr/bin:/bin",
				"HOME":                "/home/tester",
				"TMPDIR":              "/tmp/scratch",
				"SYSTEMROOT":          `C:\Windows`,
				"NODE_OPTIONS":        "--no-experimental-strip-types",
				"SHIFT2EU_TEST_OTHER": "not on the list",
				"TMPDIR_UNLISTED":     "/tmp/other",
				"SYSTEMROOT_UNLISTED": `C:\Windows\Other`,
			},
			want: []string{
				"PATH=/usr/local/node/bin:/usr/bin:/bin",
				"HOME=/home/tester",
				"TMPDIR=/tmp/scratch",
				"SYSTEMROOT=" + `C:\Windows`,
			},
		},
		{
			name: "only the two the load cases need, where the others are unset",
			inherited: map[string]string{
				"PATH":         "/usr/bin:/bin",
				"HOME":         "/home/tester",
				"NODE_OPTIONS": "--import /tmp/loader.mjs",
			},
			want: []string{
				"PATH=/usr/bin:/bin",
				"HOME=/home/tester",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := piNodeEnvFrom(func(name string) (string, bool) {
				value, ok := tc.inherited[name]

				return value, ok
			})

			if !slices.Equal(got, tc.want) {
				t.Errorf("piNodeEnvFrom(inherited) = %q, want %q: node gets the allow list and nothing else (CLAUDE.md, rule 3, issue #548)", got, tc.want)
			}
		})
	}
}

// piNodeVerdict is what the check does with the node it found.
type piNodeVerdict int

const (
	// piNodeRun is a node that strips TypeScript types by default: the load
	// cases run.
	piNodeRun piNodeVerdict = iota

	// piNodeSkip is a node that cannot load the file here, on a machine where
	// the check may be skipped (no SHIFT2EU_REQUIRE_NODE=1).
	piNodeSkip

	// piNodeFail is the same node, where SHIFT2EU_REQUIRE_NODE=1 says the check
	// has to run.
	piNodeFail
)

// String names the verdict in a test's output.
func (v piNodeVerdict) String() string {
	switch v {
	case piNodeRun:
		return "run"
	case piNodeSkip:
		return "skip"
	case piNodeFail:
		return "fail"
	default:
		return fmt.Sprintf("piNodeVerdict(%d)", int(v))
	}
}

// piNodeState is what the check knows about the node it found before it loads
// anything: whether there is one at all (found), what it calls itself
// (version), whether it strips TypeScript types by default (strips), the version
// dev/lima.yaml pins (pinned), and whether SHIFT2EU_REQUIRE_NODE=1 (required).
type piNodeState struct {
	found    bool
	version  string
	strips   bool
	pinned   string
	required bool
}

// piNodeDecision says what the check does with the node it found: run the load
// cases, skip them because this machine cannot load a .ts file and the
// environment accepts a skip, or fail because it cannot and
// SHIFT2EU_REQUIRE_NODE=1 says the check has to run. The reason names the version
// found, the one dev/lima.yaml pins and this issue, so a skip in a log says what
// to install; it is empty when the cases run. A node that cannot strip types
// never reaches them: its import would fail with ERR_UNKNOWN_FILE_EXTENSION, and
// that must not pass for a broken extension.
func piNodeDecision(state piNodeState) (piNodeVerdict, string) {
	if state.found && state.strips {
		return piNodeRun, ""
	}

	// trouble is the half both verdicts say: what the node found cannot do.
	trouble := "node is not on the PATH, so the pi deny extension cannot be loaded and checked here"
	if state.found {
		trouble = fmt.Sprintf("node %s does not strip TypeScript types on import (type stripping is on by default from Node 22.18 / 23.6), so it cannot load the pi deny extension", state.version)
	}

	install := fmt.Sprintf("install Node %s, the version dev/lima.yaml pins (issue #548, ADR 0044)", state.pinned)

	if state.required {
		return piNodeFail, trouble + ", and SHIFT2EU_REQUIRE_NODE=1 says this check has to run here: " + install
	}

	return piNodeSkip, trouble + "; " + install + ", or set SHIFT2EU_REQUIRE_NODE=1 to fail instead of skipping"
}

// piNodeEnvNames is the allow list of variables node's environment is built
// from, in the order it is written: PATH and HOME, which node needs to find
// itself and to resolve nothing else, and TMPDIR and SYSTEMROOT where the
// platform sets them. Nothing more is needed: the extension reads homedir() only
// inside its factory, which this check never calls.
var piNodeEnvNames = []string{"PATH", "HOME", "TMPDIR", "SYSTEMROOT"}

// piNodeEnv returns node's environment from the allow list above, read by name
// from the process's. The inherited environment is never passed whole, since in
// a pane it holds the agents' API keys (CLAUDE.md, rule 3); NODE_OPTIONS in
// particular is not on the list, because a loader or flag it names could decide
// whether the extension's types are stripped and so whether it loads at all. The
// check pins what the extension and the pinned Node do, not what a variable on
// one machine made them do.
func piNodeEnv() []string {
	return piNodeEnvFrom(os.LookupEnv)
}

// piNodeEnvFrom builds node's environment from the allow list, with lookup
// standing in for the process's environment so that a test can hand it one
// without setting any variable of its own.
func piNodeEnvFrom(lookup func(name string) (string, bool)) []string {
	out := make([]string, 0, len(piNodeEnvNames))

	for _, name := range piNodeEnvNames {
		if value, ok := lookup(name); ok {
			out = append(out, name+"="+value)
		}
	}

	return out
}

// piNodeVersionPin matches dev/lima.yaml's nodeVersion param, the Node the VM
// installs for pi and the one ci.yml's and release.yml's Test jobs install for
// this check. It reads the line as those jobs' sed does, two spaces and
// `nodeVersion: "X"`, never any other indentation or spacing.
var piNodeVersionPin = regexp.MustCompile(`(?m)^  nodeVersion: "([^"]+)"$`)

// piPinnedNodeVersion returns the Node version dev/lima.yaml pins, which a skip
// or a failure names as the one to install.
func piPinnedNodeVersion(t *testing.T) string {
	t.Helper()

	const lima = "dev/lima.yaml"

	match := piNodeVersionPin.FindSubmatch(readRepositoryFile(t, lima))
	if match == nil {
		t.Fatalf("%s pins no nodeVersion, so a skip cannot say which Node this check needs (issue #548)", lima)
	}

	return string(match[1])
}

// piProbeNode asks node what it calls itself and whether it strips TypeScript
// types on import, through the probe script above, and fails the test when node
// cannot answer: a node that cannot run a one-line program is a broken install,
// which is neither a machine without node nor a broken extension.
func piProbeNode(t *testing.T, nodePath string) (version string, strips bool) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, nodePath, "-e", piNodeProbeScript)
	cmd.Dir = repositoryRoot(t)
	cmd.Env = piNodeEnv()

	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("%s could not say what it supports before the timeout: %v\n%s", nodePath, ctx.Err(), out)
	}

	if err != nil {
		t.Fatalf("%s could not say what it supports, which is a broken install rather than a broken extension: %v\n%s", nodePath, err, out)
	}

	var said struct {
		Version string `json:"version"`
		Strips  bool   `json:"strips"`
	}

	if err := json.Unmarshal(out, &said); err != nil {
		t.Fatalf("%s answered %q, which is no JSON: %v (issue #548)", nodePath, out, err)
	}

	return said.Version, said.Strips
}

// piLoadExtension imports the file at path with node, through the script above,
// and returns what node said with the import's own error, if any. Everything is
// an argument: no shell runs, and no path is built into the script's text.
func piLoadExtension(t *testing.T, nodePath, path string) ([]byte, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, nodePath, "--input-type=module", "-e", piNodeLoadScript, path)
	cmd.Dir = repositoryRoot(t)
	cmd.Env = piNodeEnv()

	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("node did not load %s before the timeout: %v\n%s", path, ctx.Err(), out)
	}

	return out, err
}
