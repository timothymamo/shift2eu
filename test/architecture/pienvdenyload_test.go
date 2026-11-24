// Runs pi's refusal of the commands that print the environment (CLAUDE.md
// rule 3, ADR 0044) instead of reading it (issue #548).
//
// The static pins in pienvdeny_test.go hold the extension's text and its pattern
// list as data. None of them can see the one failure that loses the refusal
// whole: pi logs an extension that throws while it loads and carries on without
// it, so a top-level statement that throws (an invalid regular expression, a
// call whose argument a later edit got wrong) fails open, and outside a TUI pane
// nobody sees the log. `make check` therefore has to load the extension the way
// pi does and fail when it cannot.
//
// Loading is all this needs: importing the module evaluates its top level, which
// does nothing (the list is read in the factory, when pi calls it), so no refused
// command runs here and nothing is passed to a shell. Node strips the file's
// TypeScript types itself, and the `import type` of pi's package is erased, so
// the check needs no loader and no package of its own: only node, which the VM
// installs for pi (dev/lima.yaml, nodeVersion).
//
// The test runs whatever `node` the PATH holds. Where there is none (a Mac with
// no Node, a runner that has not set one up) it skips with a message naming this
// issue, and fails instead when SHIFT2EU_REQUIRE_NODE=1 is set, which is how CI
// refuses a silent skip everywhere.
package architecture_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
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

	// node is the one program this check needs, and the pinned one is in the VM
	// (dev/lima.yaml, nodeVersion). Where it is missing the check cannot run, and
	// only the environment variable that CI sets says that is not acceptable.
	nodePath, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("SHIFT2EU_REQUIRE_NODE") == "1" {
			t.Fatalf("node is not on the PATH, and SHIFT2EU_REQUIRE_NODE=1 says this check has to run here: install the Node dev/lima.yaml pins (nodeVersion) or drop the variable (issue #548)")
		}

		t.Skip("node is not on the PATH, so the pi deny extension cannot be loaded and checked here; CI sets SHIFT2EU_REQUIRE_NODE=1 and runs this test instead (issue #548, ADR 0044)")
	}

	real := readRepositoryFile(t, piEnvDenyExtension)

	// The third case loads a copy whose default export is renamed, and so has
	// none: a replacement that no longer matches would leave that copy exporting
	// the function and pin nothing.
	const defaultExport = "export default function (pi: ExtensionAPI)"

	noDefaultExport := bytes.Replace(real, []byte(defaultExport), []byte("function piExtension(pi: ExtensionAPI)"), 1)
	if bytes.Equal(noDefaultExport, real) {
		t.Fatalf("%s no longer holds %q, so the case that loads a copy without a default export pins nothing: give it the extension's default export as the file writes it (issue #548)", piEnvDenyExtension, defaultExport)
	}

	cases := []struct {
		name string
		// source is the text loaded as the extension. Empty loads pi's own file,
		// in place, as pi loads it: the copies that fail are written into a
		// temporary directory, since the file itself is never broken.
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
			source: []byte(string(real) + "\nnew RegExp(\"(\");\n"),
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
				t.Fatalf("node loaded %s, want the load to fail: this is the copy that no longer refuses anything, so a check that loads it is a check that cannot see the extension break (issue #548)\n%s", tc.name, out)
			}

			if !strings.Contains(string(out), tc.want) {
				t.Errorf("loading %s failed, but not for the reason its case pins: want node to say %q, got:\n%s", path, tc.want, out)
			}
		})
	}
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

// piNodeEnv returns node's environment: the inherited one without NODE_OPTIONS,
// which a machine can have set to a loader or to a flag that would decide
// whether the extension's types are stripped and so whether it loads at all. The
// check pins what the extension and the pinned Node do, not what a variable on
// one machine made them do.
func piNodeEnv() []string {
	inherited := os.Environ()

	out := make([]string, 0, len(inherited))

	for _, entry := range inherited {
		if name, _, _ := strings.Cut(entry, "="); name == "NODE_OPTIONS" {
			continue
		}

		out = append(out, entry)
	}

	return out
}
