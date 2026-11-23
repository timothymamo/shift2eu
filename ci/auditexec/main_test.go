// Pins the command wrapper the audit's model runs through, audit-exec (ADR
// 0040's model-job amendment, issue #277). The wrapper maps a command name to
// one fixed absolute path and execs it, refuses every other name, refuses git
// outside the subcommands and options the ADR lists, changes into its own
// working directory before every exec, and execs with the scrubbed environment
// the ADR fixes: the caller's environment reaches the command in no way, and
// neither does its working directory.
//
// The tests are white-box (package main): ci/auditexec is a main package, so
// only its own test files reach it, and the two seams it takes - the change of
// directory and the exec - are what a test hands its fakes, as
// ci/claudecodepin's entry point does. Nothing is executed: the seams record the
// directory the wrapper changed into and the path, the arguments and the
// environment it would have replaced itself with.
package main

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

const (
	// auditCheckout is the copy of the checkout the git commands run in: the
	// wrapper's own -C, not the caller's (an option the wrapper refuses).
	auditCheckout = "/srv/audit/checkout"

	// auditWork is the wrapper's fixed working directory: where it changes into
	// before every exec, and where a relative name on a line is read from.
	auditWork = "/srv/audit/work"

	// refusedExit is the status the wrapper ends with for a refused command
	// name, a refused git subcommand and a refused option alike.
	refusedExit = 126

	// failedExit is the status the wrapper ends with when it could not take the
	// step before the command: a changed directory that failed, like an exec
	// that did.
	failedExit = 127
)

// scrubbedEnv is the whole environment the wrapper execs a command with, as the
// ADR fixes it: PATH, HOME, TMPDIR, LANG, LC_ALL and TZ, and the five variables
// that keep git off the caller's configuration, its terminal, its pager and its
// optional locks. Nothing of the caller's is inherited, so this list is the
// whole of what a command sees.
var scrubbedEnv = []string{
	"PATH=/usr/bin:/bin",
	"HOME=/nonexistent",
	"TMPDIR=/srv/audit/work/tmp",
	"LANG=C.UTF-8",
	"LC_ALL=C.UTF-8",
	"TZ=UTC",
	"GIT_CONFIG_NOSYSTEM=1",
	"GIT_CONFIG_GLOBAL=/dev/null",
	"GIT_TERMINAL_PROMPT=0",
	"GIT_PAGER=cat",
	"GIT_OPTIONAL_LOCKS=0",
}

// executed is what the wrapper asked the exec seam to run.
type executed struct {
	path string
	argv []string
	env  []string
}

// execRecorder is the fake exec seam: it records the one call the wrapper makes
// and answers with its own error, so a run that got as far as exec'ing is a
// run the test can read.
type execRecorder struct {
	called bool
	call   executed
	err    error
}

// exec is the seam's shape, which syscall.Exec has: the wrapper replaces itself
// with path, and the fake records what it would have been replaced with.
func (r *execRecorder) exec(path string, argv, env []string) error {
	r.called = true
	r.call = executed{path: path, argv: argv, env: env}

	return r.err
}

// seamRecorder is the fake pair of seams the wrapper takes: it records every
// call the wrapper makes in order - "chdir <dir>" and "exec <path>" - and
// answers with its own errors, so a test can read whether the change of
// directory came first.
type seamRecorder struct {
	steps    []string
	chdirErr error
	execErr  error
	executed []executed
}

// chdir is the fake change of directory: the wrapper's own, before the exec.
func (r *seamRecorder) chdir(dir string) error {
	r.steps = append(r.steps, "chdir "+dir)

	return r.chdirErr
}

// exec is the fake exec seam: it records what the wrapper would have replaced
// itself with.
func (r *seamRecorder) exec(path string, argv, env []string) error {
	r.steps = append(r.steps, "exec "+path)
	r.executed = append(r.executed, executed{path: path, argv: argv, env: env})

	return r.execErr
}

// dirFailed is the failure a change of directory answers with when the
// directory cannot be entered.
type dirFailed struct{}

// Error implements error.
func (dirFailed) Error() string { return "no such file or directory" }

// runWrapper runs the wrapper's logic with args and the fake seams, and returns
// its exit status, its stderr and the exec fake.
func runWrapper(t *testing.T, args []string) (int, string, *execRecorder) {
	t.Helper()

	recorder := &execRecorder{}

	var stderr bytes.Buffer

	code := run(args, func(string) error { return nil }, recorder.exec, &stderr)

	return code, stderr.String(), recorder
}

// TestRunExecsTheListedCommands pins the commands the wrapper may exec and the
// path each one runs from: the fixed absolute path the ADR names, never a
// lookup through PATH, and nothing else on the line. The environment is the
// scrubbed allow list, and no name of it comes from the caller.
func TestRunExecsTheListedCommands(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// args are the wrapper's arguments, the command name first.
		args []string
		// wantPath is the fixed path the name maps to.
		wantPath string
		// wantArgs are the arguments the command runs with, the executable's
		// own path included: for git, the wrapper's --no-pager and -C come
		// before the caller's subcommand.
		wantArgs []string
	}{
		{
			name:     "git log",
			args:     []string{"git", "log", "-1", "--format=%H"},
			wantPath: "/usr/bin/git",
			wantArgs: []string{"/usr/bin/git", "--no-pager", "-C", auditCheckout, "log", "-1", "--format=%H"},
		},
		{
			name:     "git show",
			args:     []string{"git", "show", "HEAD"},
			wantPath: "/usr/bin/git",
			wantArgs: []string{"/usr/bin/git", "--no-pager", "-C", auditCheckout, "show", "HEAD"},
		},
		{
			name:     "git diff",
			args:     []string{"git", "diff", "HEAD~1", "HEAD"},
			wantPath: "/usr/bin/git",
			wantArgs: []string{"/usr/bin/git", "--no-pager", "-C", auditCheckout, "diff", "HEAD~1", "HEAD"},
		},
		{
			name:     "git grep",
			args:     []string{"git", "grep", "-n", "token"},
			wantPath: "/usr/bin/git",
			wantArgs: []string{"/usr/bin/git", "--no-pager", "-C", auditCheckout, "grep", "-n", "token"},
		},
		{
			name:     "git ls-files",
			args:     []string{"git", "ls-files"},
			wantPath: "/usr/bin/git",
			wantArgs: []string{"/usr/bin/git", "--no-pager", "-C", auditCheckout, "ls-files"},
		},
		{
			name:     "git ls-tree",
			args:     []string{"git", "ls-tree", "-r", "HEAD"},
			wantPath: "/usr/bin/git",
			wantArgs: []string{"/usr/bin/git", "--no-pager", "-C", auditCheckout, "ls-tree", "-r", "HEAD"},
		},
		{
			name:     "git cat-file",
			args:     []string{"git", "cat-file", "-p", "HEAD"},
			wantPath: "/usr/bin/git",
			wantArgs: []string{"/usr/bin/git", "--no-pager", "-C", auditCheckout, "cat-file", "-p", "HEAD"},
		},
		{
			name:     "git rev-parse",
			args:     []string{"git", "rev-parse", "HEAD"},
			wantPath: "/usr/bin/git",
			wantArgs: []string{"/usr/bin/git", "--no-pager", "-C", auditCheckout, "rev-parse", "HEAD"},
		},
		{
			name:     "git rev-list",
			args:     []string{"git", "rev-list", "--count", "HEAD"},
			wantPath: "/usr/bin/git",
			wantArgs: []string{"/usr/bin/git", "--no-pager", "-C", auditCheckout, "rev-list", "--count", "HEAD"},
		},
		{
			name:     "git blame",
			args:     []string{"git", "blame", "main.go"},
			wantPath: "/usr/bin/git",
			wantArgs: []string{"/usr/bin/git", "--no-pager", "-C", auditCheckout, "blame", "main.go"},
		},
		{
			name:     "git shortlog",
			args:     []string{"git", "shortlog", "-sn", "HEAD"},
			wantPath: "/usr/bin/git",
			wantArgs: []string{"/usr/bin/git", "--no-pager", "-C", auditCheckout, "shortlog", "-sn", "HEAD"},
		},
		{
			name:     "git describe",
			args:     []string{"git", "describe", "--tags"},
			wantPath: "/usr/bin/git",
			wantArgs: []string{"/usr/bin/git", "--no-pager", "-C", auditCheckout, "describe", "--tags"},
		},
		{
			name:     "git for-each-ref",
			args:     []string{"git", "for-each-ref", "refs/tags"},
			wantPath: "/usr/bin/git",
			wantArgs: []string{"/usr/bin/git", "--no-pager", "-C", auditCheckout, "for-each-ref", "refs/tags"},
		},
		{
			name:     "jq",
			args:     []string{"jq", "-r", ".findings"},
			wantPath: "/usr/bin/jq",
			wantArgs: []string{"/usr/bin/jq", "-r", ".findings"},
		},
		{
			name:     "sha256sum",
			args:     []string{"sha256sum", "/srv/audit/work/result.json"},
			wantPath: "/usr/bin/sha256sum",
			wantArgs: []string{"/usr/bin/sha256sum", "/srv/audit/work/result.json"},
		},
		{
			name:     "base64",
			args:     []string{"base64", "-w0"},
			wantPath: "/usr/bin/base64",
			wantArgs: []string{"/usr/bin/base64", "-w0"},
		},
		{
			name:     "od",
			args:     []string{"od", "-c", "/srv/audit/checks/x"},
			wantPath: "/usr/bin/od",
			wantArgs: []string{"/usr/bin/od", "-c", "/srv/audit/checks/x"},
		},
		{
			name:     "cat",
			args:     []string{"cat", "/srv/audit/work/preflight-control.txt"},
			wantPath: "/usr/bin/cat",
			wantArgs: []string{"/usr/bin/cat", "/srv/audit/work/preflight-control.txt"},
		},
		{
			name:     "head",
			args:     []string{"head", "-n", "20", "/srv/audit/work/audit.jsonl"},
			wantPath: "/usr/bin/head",
			wantArgs: []string{"/usr/bin/head", "-n", "20", "/srv/audit/work/audit.jsonl"},
		},
		{
			name:     "tail",
			args:     []string{"tail", "-n", "20", "/srv/audit/work/audit.jsonl"},
			wantPath: "/usr/bin/tail",
			wantArgs: []string{"/usr/bin/tail", "-n", "20", "/srv/audit/work/audit.jsonl"},
		},
		{
			name:     "wc",
			args:     []string{"wc", "-l"},
			wantPath: "/usr/bin/wc",
			wantArgs: []string{"/usr/bin/wc", "-l"},
		},
		{
			name:     "sort",
			args:     []string{"sort", "-u"},
			wantPath: "/usr/bin/sort",
			wantArgs: []string{"/usr/bin/sort", "-u"},
		},
		{
			name:     "uniq",
			args:     []string{"uniq", "-c"},
			wantPath: "/usr/bin/uniq",
			wantArgs: []string{"/usr/bin/uniq", "-c"},
		},
		{
			name:     "cut",
			args:     []string{"cut", "-d", ":", "-f", "1"},
			wantPath: "/usr/bin/cut",
			wantArgs: []string{"/usr/bin/cut", "-d", ":", "-f", "1"},
		},
		{
			name:     "tr",
			args:     []string{"tr", "-d", "\\r"},
			wantPath: "/usr/bin/tr",
			wantArgs: []string{"/usr/bin/tr", "-d", "\\r"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			code, stderr, recorder := runWrapper(t, tc.args)

			if code != 0 {
				t.Fatalf("run(%q) = %d, want 0 (stderr %q): the wrapper execs it and the exec seam answered", tc.args, code, stderr)
			}

			if !recorder.called {
				t.Fatalf("run(%q) never reached the exec seam: a listed command is exec'd", tc.args)
			}

			if recorder.call.path != tc.wantPath {
				t.Errorf("run(%q) execs %q, want %q: a command runs from its fixed absolute path, never through PATH", tc.args, recorder.call.path, tc.wantPath)
			}

			if diff := cmp.Diff(tc.wantArgs, recorder.call.argv); diff != "" {
				t.Errorf("run(%q) execs it with these arguments (-want +got):\n%s", tc.args, diff)
			}

			want := slices.Clone(scrubbedEnv)
			got := slices.Clone(recorder.call.env)

			slices.Sort(want)
			slices.Sort(got)

			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("run(%q) execs it with this environment (-want +got):\n%s", tc.args, diff)
			}
		})
	}
}

// TestRunRefusesEveryOtherName pins the names the wrapper refuses: every
// program that is not on its list, a path given as the name included, and no
// name at all. Each ends the run with 126, one line on stderr, and no exec.
func TestRunRefusesEveryOtherName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args []string
		// wantNamed is the word the one stderr line names.
		wantNamed string
	}{
		{name: "no name at all", args: nil},
		{name: "sh", args: []string{"sh", "-c", "true"}, wantNamed: "sh"},
		{name: "bash", args: []string{"bash"}, wantNamed: "bash"},
		{name: "python3", args: []string{"python3"}, wantNamed: "python3"},
		{name: "nc, which is in no allow list", args: []string{"nc", "127.0.0.1", "80"}, wantNamed: "nc"},
		{name: "curl", args: []string{"curl", "https://example.com"}, wantNamed: "curl"},
		{name: "a path to a listed command", args: []string{"/usr/bin/git", "log"}, wantNamed: "/usr/bin/git"},
		{name: "a relative path to a listed command", args: []string{"./git", "log"}, wantNamed: "./git"},
		{name: "a listed name with a suffix", args: []string{"git2", "log"}, wantNamed: "git2"},
		{name: "a listed name in another case", args: []string{"GIT", "log"}, wantNamed: "GIT"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			code, stderr, recorder := runWrapper(t, tc.args)

			if code != refusedExit {
				t.Errorf("run(%q) = %d, want %d: every name but a listed one is refused", tc.args, code, refusedExit)
			}

			if recorder.called {
				t.Errorf("run(%q) exec'd %q; a refused name runs nothing", tc.args, recorder.call.path)
			}

			if got := strings.Count(stderr, "\n"); got != 1 {
				t.Errorf("run(%q) wrote %d lines to stderr (%q), want one naming the refused word", tc.args, got, stderr)
			}

			if tc.wantNamed != "" && !strings.Contains(stderr, tc.wantNamed) {
				t.Errorf("run(%q) wrote %q to stderr, want it to name the refused word %q", tc.args, stderr, tc.wantNamed)
			}
		})
	}
}

// TestRunRefusesEveryOtherGitSubcommand pins that git runs only the subcommands
// the ADR lists: the ones that write, the ones that reach the network and the
// ones that read another repository are refused before git starts, so the
// wrapper's own -C is the only repository git can be pointed at.
func TestRunRefusesEveryOtherGitSubcommand(t *testing.T) {
	t.Parallel()

	for _, subcommand := range []string{
		"push", "commit", "tag", "remote", "fetch", "pull", "clone", "checkout",
		"switch", "reset", "clean", "config", "bisect", "notes", "reflog",
		"submodule", "worktree", "gc", "prune", "filter-branch", "bundle",
	} {
		t.Run("git "+subcommand, func(t *testing.T) {
			t.Parallel()

			code, stderr, recorder := runWrapper(t, []string{"git", subcommand})

			if code != refusedExit {
				t.Errorf("run([git %s]) = %d, want %d: only the ADR's subcommands run", subcommand, code, refusedExit)
			}

			if recorder.called {
				t.Errorf("run([git %s]) exec'd %q; a refused subcommand runs nothing", subcommand, recorder.call.argv)
			}

			if !strings.Contains(stderr, subcommand) {
				t.Errorf("run([git %s]) wrote %q to stderr, want it to name the refused word", subcommand, stderr)
			}
		})
	}

	t.Run("git with no subcommand", func(t *testing.T) {
		t.Parallel()

		code, _, recorder := runWrapper(t, []string{"git"})

		if code != refusedExit {
			t.Errorf("run([git]) = %d, want %d: git runs with one of the listed subcommands", code, refusedExit)
		}

		if recorder.called {
			t.Errorf("run([git]) exec'd %q; git with no subcommand runs nothing", recorder.call.argv)
		}
	})
}

// TestRunRefusesTheOptionsThatWriteOrReadElsewhere pins the git and sort options
// the ADR refuses anywhere on the line, in every position and in the
// --opt=value form: an option that names another git directory, another work
// tree, another configuration, another pager or an output file would give the
// wrapper's containment away.
func TestRunRefusesTheOptionsThatWriteOrReadElsewhere(t *testing.T) {
	t.Parallel()

	// gitRefusedOptions are the git options the wrapper refuses wherever they
	// appear, with the value a case gives them.
	gitRefusedOptions := []struct {
		option string
		value  string
	}{
		{"-c", "core.pager=less"},
		{"--config-env", "core.pager=PAGER"},
		{"--exec-path", "/tmp"},
		{"--git-dir", "/srv/audit/checkout/.git"},
		{"--work-tree", "/etc"},
		{"--namespace", "other"},
		// The options that name the repository, the work tree or a path into
		// the objects are refused whatever their value, a value inside the
		// roots included (issue #277, round H, finding 1): the caller's -C has
		// its own rows below, and these are its equals.
		{"--super-prefix", "/srv/audit/checkout/"},
		{"--attr-source", "HEAD"},
		{"--bare", ""},
		{"--literal-pathspecs", ""},
		{"--ext-diff", ""},
		{"--textconv", ""},
		{"--output", "/srv/audit/state/probe"},
		// Verifying a signature runs gpg, the program gpg.program names, and
		// git reads a long option's unique prefix as the option itself (issue
		// #277, round D finding 2).
		{"--show-signature", ""},
		{"--show-sig", ""},
		// Comparing two files of its own is no read of the checkout (issue
		// #277, round G).
		{"--no-index", ""},
	}

	for _, refused := range gitRefusedOptions {
		t.Run("git "+refused.option, func(t *testing.T) {
			t.Parallel()

			cases := []struct {
				name string
				args []string
			}{
				{
					name: "before the subcommand",
					args: append([]string{"git", refused.option}, append(valueArgs(refused.value), "log")...),
				},
				{
					name: "after the subcommand",
					args: append([]string{"git", "log"}, append(valueArgs(refused.value), refused.option)...),
				},
				{
					// The option sits on the line, between the subcommand and
					// its own argument: what the wrapper must refuse.
					name: "between the subcommand and its arguments",
					args: append(append([]string{"git", "log", refused.option}, valueArgs(refused.value)...), "-1"),
				},
				{
					name: "in the option's own =value form",
					args: append([]string{"git", "log", refused.option + "=" + refused.value}, "-1"),
				},
			}

			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()

					code, stderr, recorder := runWrapper(t, tc.args)

					if code != refusedExit {
						t.Errorf("run(%q) = %d, want %d: %s is refused anywhere on the line", tc.args, code, refusedExit, refused.option)
					}

					if recorder.called {
						t.Errorf("run(%q) exec'd %q; a refused option runs nothing", tc.args, recorder.call.argv)
					}

					if !strings.Contains(stderr, refused.option) {
						t.Errorf("run(%q) wrote %q to stderr, want it to name the refused option %s", tc.args, stderr, refused.option)
					}
				})
			}
		})
	}

	// git diff's --no-index compares two files of its own, inside the three roots
	// or anywhere else, with no repository: it is a read of files that are no
	// part of the checkout, so the option is refused - abbreviated, as the other
	// long options are - while both of its paths are inside the roots and the
	// path rule alone would pass them (issue #277, round G).
	t.Run("git diff --no-index", func(t *testing.T) {
		t.Parallel()

		for _, args := range [][]string{
			{"git", "diff", "--no-index", "/srv/audit/work/x", "/srv/audit/work/y"},
			{"git", "diff", "--no-ind", "/srv/audit/work/x", "/srv/audit/work/y"},
			{"git", "diff", "/srv/audit/work/x", "--no-index", "/srv/audit/work/y"},
			{"git", "diff", "--no-index=/srv/audit/work/x", "/srv/audit/work/y"},
		} {
			code, stderr, recorder := runWrapper(t, args)

			if code != refusedExit {
				t.Errorf("run(%q) = %d, want %d: git diff --no-index compares files of its own, no part of the checkout", args, code, refusedExit)
			}

			if recorder.called {
				t.Errorf("run(%q) exec'd %q; a refused option runs nothing", args, recorder.call.argv)
			}

			if !strings.Contains(stderr, "--no-ind") {
				t.Errorf("run(%q) wrote %q to stderr, want it to name the refused option", args, stderr)
			}
		}
	})

	// A caller's -C is refused outright (issue #277, round H, finding 1): the
	// wrapper's own -C /srv/audit/checkout comes first, but git applies the -C
	// options in order, so a later one would point git wherever the caller
	// likes - /srv/audit/work, which is an allowed root the model may write
	// into, included. A .git/config the model planted there names
	// core.fsmonitor or diff.external, and git runs that program. Every form
	// is refused, git log's -C (copy detection) among them: -C is ambiguous on
	// the line, and the check fails closed.
	t.Run("git's caller -C", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name string
			args []string
			// wantNamed is the word the one stderr line names: the whole word,
			// the option's value attached included.
			wantNamed string
		}{
			{
				name:      "the next word as its value, in the work area",
				args:      []string{"git", "-C", auditWork, "diff"},
				wantNamed: "-C",
			},
			{
				name:      "its value attached, in the work area",
				args:      []string{"git", "-C" + auditWork, "log"},
				wantNamed: "-C" + auditWork,
			},
			{
				name:      "the relative current directory",
				args:      []string{"git", "-C", ".", "log"},
				wantNamed: "-C",
			},
			{
				// The value lies inside the roots, so round G's path rule
				// passed it: the wrapper's own -C is the only one git reads.
				name:      "the checkout itself",
				args:      []string{"git", "-C", auditCheckout, "log"},
				wantNamed: "-C",
			},
			{
				// git log's own -C is copy detection, no -C of the caller's. The
				// word is refused all the same: it is ambiguous on the line.
				name:      "git log's copy detection",
				args:      []string{"git", "log", "-C"},
				wantNamed: "-C",
			},
			{
				// git reads a short option one letter at a time, so the letter
				// is refused in a cluster as at its start.
				name:      "in a short-option cluster",
				args:      []string{"git", "log", "-nC"},
				wantNamed: "-nC",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				code, stderr, recorder := runWrapper(t, tc.args)

				if code != refusedExit {
					t.Errorf("run(%q) = %d, want %d: a caller's -C points git away from the checkout, at a work area the model writes in (stderr %q)", tc.args, code, refusedExit, stderr)
				}

				if recorder.called {
					t.Errorf("run(%q) exec'd %q; a refused -C runs nothing", tc.args, recorder.call.argv)
				}

				if !strings.Contains(stderr, tc.wantNamed) {
					t.Errorf("run(%q) wrote %q to stderr, want it to name the refused word %q", tc.args, stderr, tc.wantNamed)
				}
			})
		}
	})

	// jq's module system is refused (issue #277, round H, finding 2): a filter
	// that imports or includes a module, and the -L and --library-path options
	// that name where jq looks for one, let it read any .json file of the
	// image - the results it writes included - so the audit takes filters, no
	// modules. Every word holding import, include, modulemeta or get_search_list
	// is refused, so a word that merely mentions one is too: the check fails
	// closed.
	t.Run("jq's module system", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name string
			args []string
			// wantNamed is the word the one stderr line names.
			wantNamed string
		}{
			{
				name:      "a filter that imports a module",
				args:      []string{"jq", "-n", `import "x" as $d {search: "/etc"}; $d`},
				wantNamed: "import",
			},
			{
				name:      "a filter that includes a module",
				args:      []string{"jq", "-n", `include "x"; 1`},
				wantNamed: "include",
			},
			{
				name:      "-L with its value attached",
				args:      []string{"jq", "-Lx", "-n", "1"},
				wantNamed: "-Lx",
			},
			{
				name:      "-L with its value attached, inside the roots",
				args:      []string{"jq", "-L" + auditWork, "-n", "1"},
				wantNamed: "-L" + auditWork,
			},
			{
				name:      "--library-path with its value as the next word",
				args:      []string{"jq", "--library-path", auditWork, "-n", "1"},
				wantNamed: "--library-path",
			},
			{
				name:      "--library-path in its =value form",
				args:      []string{"jq", "--library-path=" + auditWork, "-n", "1"},
				wantNamed: "--library-path=" + auditWork,
			},
			{
				name:      "a filter that imports a module after a path out of the roots' kin",
				args:      []string{"jq", "-n", `{search: "../state/home"} | import "x" as $d; 1`},
				wantNamed: "import",
			},

			// jq's two builtins that read a module's metadata are the module
			// system by another name (issue #277, round L): modulemeta searches
			// the paths it is given, and get_search_list reports the paths the
			// search took, so each reads a .json file the line never named.
			// They are refused words of the module check, which reads every
			// word of the line.
			{
				name:      "a filter that asks for a module's metadata",
				args:      []string{"jq", "-n", `"x"|modulemeta`},
				wantNamed: "modulemeta",
			},
			{
				name:      "a filter that asks for the search list",
				args:      []string{"jq", "-n", `{"a":{}}|get_search_list`},
				wantNamed: "get_search_list",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				code, stderr, recorder := runWrapper(t, tc.args)

				if code != refusedExit {
					t.Errorf("run(%q) = %d, want %d: jq reads the filters of the line, no module and no library path (stderr %q)", tc.args, code, refusedExit, stderr)
				}

				if recorder.called {
					t.Errorf("run(%q) exec'd %q; a refused line runs nothing", tc.args, recorder.call.argv)
				}

				if !strings.Contains(stderr, tc.wantNamed) {
					t.Errorf("run(%q) wrote %q to stderr, want it to name the refused word %q", tc.args, stderr, tc.wantNamed)
				}
			})
		}
	})

	// jq reads no program file (issue #277, round I): -f and --from-file name a
	// file holding a jq program, and a program's own import and include are
	// never read by the module check above, which sees the line alone - so a
	// program file is the module system by another name. f is refused in any
	// single-dash word - alone, with the file attached (-fp.jq), in a cluster
	// (-nf) - and --from-file alone, in its =value form and abbreviated. The
	// audit's filters come on the line.
	t.Run("jq's program file", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name string
			args []string
			// wantNamed is the word the one stderr line names.
			wantNamed string
		}{
			{
				name:      "-f in a cluster of allowed letters",
				args:      []string{"jq", "-nf", "p.jq"},
				wantNamed: "-nf",
			},
			{
				name:      "-f alone",
				args:      []string{"jq", "-n", "-f", "p.jq"},
				wantNamed: "-f",
			},
			{
				name:      "-f with the file attached",
				args:      []string{"jq", "-fp.jq", "-n"},
				wantNamed: "-fp.jq",
			},
			{
				name:      "-f with a file inside the roots",
				args:      []string{"jq", "-f", "/srv/audit/work/p.jq", "-n"},
				wantNamed: "-f",
			},
			{
				name:      "--from-file in its =value form",
				args:      []string{"jq", "--from-file=p.jq", "-n"},
				wantNamed: "--from-file=p.jq",
			},
			{
				name:      "--from-file with the file as the next word",
				args:      []string{"jq", "--from-file", "p.jq"},
				wantNamed: "--from-file",
			},
			{
				name:      "--from-f, an abbreviation of --from-file",
				args:      []string{"jq", "--from-f", "p.jq"},
				wantNamed: "--from-f",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				code, stderr, recorder := runWrapper(t, tc.args)

				if code != refusedExit {
					t.Errorf("run(%q) = %d, want %d: jq reads the filters of the line, no program file (stderr %q)", tc.args, code, refusedExit, stderr)
				}

				if recorder.called {
					t.Errorf("run(%q) exec'd %q; a refused line runs nothing", tc.args, recorder.call.argv)
				}

				if !strings.Contains(stderr, tc.wantNamed) {
					t.Errorf("run(%q) wrote %q to stderr, want it to name the refused word %q", tc.args, stderr, tc.wantNamed)
				}
			})
		}
	})

	// No command reads a list of paths from a file (issue #277, round I): the
	// path rule reads the words of the line, and these options make a command
	// open the paths a file names instead, so the rule never sees them. wc's
	// --files0-from is refused alone, with its value as the next word and
	// abbreviated; sha256sum's -c and --check, which read the digests' file
	// names, alone and inside a cluster.
	t.Run("a command that reads its paths from a file", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name string
			args []string
			// wantNamed is the word the one stderr line names.
			wantNamed string
		}{
			{
				name:      "wc --files0-from in its =value form",
				args:      []string{"wc", "--files0-from=list"},
				wantNamed: "--files0-from",
			},
			{
				name:      "wc --files0-from with its value as the next word",
				args:      []string{"wc", "--files0-from", "list"},
				wantNamed: "--files0-from",
			},
			{
				name:      "wc --files0, an abbreviation of --files0-from",
				args:      []string{"wc", "--files0", "list"},
				wantNamed: "--files0",
			},
			{
				name:      "sha256sum -c",
				args:      []string{"sha256sum", "-c", "sums"},
				wantNamed: "-c",
			},
			{
				name:      "sha256sum --check",
				args:      []string{"sha256sum", "--check", "sums"},
				wantNamed: "--check",
			},
			{
				name:      "sha256sum -c in a cluster",
				args:      []string{"sha256sum", "-bc", "sums"},
				wantNamed: "-bc",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				code, stderr, recorder := runWrapper(t, tc.args)

				if code != refusedExit {
					t.Errorf("run(%q) = %d, want %d: the command reads its paths from the line alone (stderr %q)", tc.args, code, refusedExit, stderr)
				}

				if recorder.called {
					t.Errorf("run(%q) exec'd %q; a refused line runs nothing", tc.args, recorder.call.argv)
				}

				if !strings.Contains(stderr, tc.wantNamed) {
					t.Errorf("run(%q) wrote %q to stderr, want it to name the refused word %q", tc.args, stderr, tc.wantNamed)
				}
			})
		}
	})

	t.Run("sort -o", func(t *testing.T) {
		t.Parallel()

		for _, args := range [][]string{
			{"sort", "-o", "/srv/audit/state/probe", "/srv/audit/work/x"},
			{"sort", "/srv/audit/work/x", "-o", "/srv/audit/state/probe"},
			{"sort", "--output=/srv/audit/state/probe"},
			{"sort", "--output", "/srv/audit/state/probe"},
			{"sort", "-o/srv/audit/state/probe"},
		} {
			code, _, recorder := runWrapper(t, args)

			if code != refusedExit {
				t.Errorf("run(%q) = %d, want %d: sort writes no file of its own", args, code, refusedExit)
			}

			if recorder.called {
				t.Errorf("run(%q) exec'd %q; a refused option runs nothing", args, recorder.call.argv)
			}
		}
	})

	// git reads a short option one letter at a time, so a cluster of them holds
	// the refused letters wherever they are: -iO<command> runs a shell command,
	// and -c<file> sets a configuration value, in the middle of a cluster as at
	// its start (issue #277, review R1).
	t.Run("git short-option clusters holding O or c", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name string
			// args hold the refused letter in a cluster, with the value git
			// would read attached, separate or beside other letters.
			args []string
			// wantNamed is the word the stderr line names.
			wantNamed string
		}{
			{
				name:      "-O alone",
				args:      []string{"git", "grep", "-O", "sh"},
				wantNamed: "-O",
			},
			{
				name:      "-O last in a cluster, its command attached",
				args:      []string{"git", "grep", "-iOsh", "x"},
				wantNamed: "-iOsh",
			},
			{
				name:      "-O last in a cluster of two",
				args:      []string{"git", "grep", "-nO", "sh", "x"},
				wantNamed: "-nO",
			},
			{
				name:      "-O in the middle of a cluster",
				args:      []string{"git", "grep", "-inOsh", "x"},
				wantNamed: "-inOsh",
			},
			{
				name:      "-c in a cluster, its value attached",
				args:      []string{"git", "log", "-iccore.pager=less"},
				wantNamed: "-iccore.pager=less",
			},
			{
				name:      "-c last in a cluster",
				args:      []string{"git", "log", "-nc"},
				wantNamed: "-nc",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				code, stderr, recorder := runWrapper(t, tc.args)

				if code != refusedExit {
					t.Errorf("run(%q) = %d, want %d: git's -O and -c are refused wherever a short-option cluster holds them", tc.args, code, refusedExit)
				}

				if recorder.called {
					t.Errorf("run(%q) exec'd %q; a refused option runs nothing", tc.args, recorder.call.argv)
				}

				if !strings.Contains(stderr, tc.wantNamed) {
					t.Errorf("run(%q) wrote %q to stderr, want it to name the refused word %q", tc.args, stderr, tc.wantNamed)
				}
			})
		}
	})

	// git's signature output is refused too (issue #277, round D finding 2): a
	// format placeholder of %G asks git for what it made of a commit's
	// signature, which is the same verification that runs gpg, and the answer
	// says nothing a report of the tree needs. Every spelling of the placeholder
	// is refused wherever a word holds it - with git's +, - and space modifiers,
	// in a plain --format, in shortlog's --group, in an abbreviated option and in
	// for-each-ref's signature atom - so a word that merely mentions %G, a grep
	// pattern among them, is refused too: the check fails closed (issue #277,
	// round E finding 2).
	t.Run("git's signature-verification format", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name string
			args []string
			// wantNamed is the word the one stderr line names.
			wantNamed string
		}{
			{
				name:      "--format=%G?",
				args:      []string{"git", "log", "--format=%G?", "-1"},
				wantNamed: "--format=%G?",
			},
			{
				name:      "--pretty=format:%GS",
				args:      []string{"git", "show", "--pretty=format:%GS", "HEAD"},
				wantNamed: "--pretty=format:%GS",
			},
			{
				name:      "--pretty=tformat:%GK",
				args:      []string{"git", "log", "--pretty=tformat:%GK"},
				wantNamed: "--pretty=tformat:%GK",
			},
			{
				name:      "--pretty=format:%GG, the raw signature output",
				args:      []string{"git", "log", "--pretty=format:%GG"},
				wantNamed: "--pretty=format:%GG",
			},
			{
				name:      "--format=%+G?, the placeholder with git's + modifier",
				args:      []string{"git", "log", "--format=%+G?", "-1"},
				wantNamed: "--format=%+G?",
			},
			{
				name:      "--pretty=format:%-GS, the placeholder with git's - modifier",
				args:      []string{"git", "show", "--pretty=format:%-GS", "HEAD"},
				wantNamed: "--pretty=format:%-GS",
			},
			{
				name:      "--format with % GK as its value, the placeholder with a space",
				args:      []string{"git", "log", "--format", "% GK", "-1"},
				wantNamed: "% GK",
			},
			{
				name:      "shortlog's --group=format:%G?",
				args:      []string{"git", "shortlog", "--group=format:%G?", "HEAD"},
				wantNamed: "--group=format:%G?",
			},
			{
				name:      "shortlog's --gr=format:%GS, an abbreviation of --group",
				args:      []string{"git", "shortlog", "--gr=format:%GS", "HEAD"},
				wantNamed: "--gr=format:%GS",
			},
			{
				name:      "for-each-ref's --sort=signature",
				args:      []string{"git", "for-each-ref", "--sort=signature"},
				wantNamed: "--sort=signature",
			},
			{
				name:      "for-each-ref's --sort=-signature",
				args:      []string{"git", "for-each-ref", "--sort=-signature"},
				wantNamed: "--sort=-signature",
			},
			{
				name:      "for-each-ref's --sort with signature:grade as its value",
				args:      []string{"git", "for-each-ref", "--sort", "signature:grade"},
				wantNamed: "signature:grade",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				code, stderr, recorder := runWrapper(t, tc.args)

				if code != refusedExit {
					t.Errorf("run(%q) = %d, want %d: a format that asks for a signature's verification is refused", tc.args, code, refusedExit)
				}

				if recorder.called {
					t.Errorf("run(%q) exec'd %q; a refused option runs nothing", tc.args, recorder.call.argv)
				}

				if !strings.Contains(stderr, tc.wantNamed) {
					t.Errorf("run(%q) wrote %q to stderr, want it to name the refused word %q", tc.args, stderr, tc.wantNamed)
				}
			})
		}
	})

	// sort's options are an allow list: every option that could make it run a
	// program, read another file or write one is refused, abbreviated long
	// options included (issue #277, review R1 and audit A2).
	t.Run("sort options outside the allow list", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name      string
			args      []string
			wantNamed string
		}{
			{
				name:      "--compress-program runs a program of the caller's",
				args:      []string{"sort", "--compress-program=sh", "/srv/audit/work/x"},
				wantNamed: "--compress-program=sh",
			},
			{
				name:      "--compress abbreviated",
				args:      []string{"sort", "--compress=sh", "/srv/audit/work/x"},
				wantNamed: "--compress=sh",
			},
			{
				name:      "--co, the shortest abbreviation sort reads",
				args:      []string{"sort", "--co=sh", "/srv/audit/work/x"},
				wantNamed: "--co=sh",
			},
			{
				name:      "--files0-from reads the names from another file",
				args:      []string{"sort", "--files0-from=-"},
				wantNamed: "--files0-from=-",
			},
			{
				name:      "--random-source reads its keys from another file",
				args:      []string{"sort", "--random-source=/srv/audit/state/probe", "/srv/audit/work/x"},
				wantNamed: "--random-source=/srv/audit/state/probe",
			},
			{
				name:      "-T with its directory as the next argument",
				args:      []string{"sort", "-T", "/srv/audit/state", "/srv/audit/work/x"},
				wantNamed: "-T",
			},
			{
				name:      "-T with its directory attached",
				args:      []string{"sort", "-T/srv/audit/state", "/srv/audit/work/x"},
				wantNamed: "-T/srv/audit/state",
			},
			{
				name:      "-T in a cluster of allowed letters",
				args:      []string{"sort", "-nT/srv/audit/state", "/srv/audit/work/x"},
				wantNamed: "-nT/srv/audit/state",
			},
			{
				name:      "--temporary-directory",
				args:      []string{"sort", "--temporary-directory=/srv/audit/state", "/srv/audit/work/x"},
				wantNamed: "--temporary-directory=/srv/audit/state",
			},
			{
				name:      "-m, which is no option of the allow list",
				args:      []string{"sort", "-m", "/srv/audit/work/x", "/srv/audit/work/y"},
				wantNamed: "-m",
			},
			{
				name:      "-S, which sizes a buffer of sort's own",
				args:      []string{"sort", "-S", "1M", "/srv/audit/work/x"},
				wantNamed: "-S",
			},
			{
				name:      "--batch-size, which is no option of the allow list either",
				args:      []string{"sort", "--batch-size=32", "/srv/audit/work/x"},
				wantNamed: "--batch-size=32",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				code, stderr, recorder := runWrapper(t, tc.args)

				if code != refusedExit {
					t.Errorf("run(%q) = %d, want %d: sort takes the ADR's short allow list and its long forms alone", tc.args, code, refusedExit)
				}

				if recorder.called {
					t.Errorf("run(%q) exec'd %q; a refused option runs nothing", tc.args, recorder.call.argv)
				}

				if !strings.Contains(stderr, tc.wantNamed) {
					t.Errorf("run(%q) wrote %q to stderr, want it to name the refused word %q", tc.args, stderr, tc.wantNamed)
				}
			})
		}
	})

	// uniq's second operand is its OUT: the wrapper runs uniq with one operand
	// or none, so no line writes a file (issue #277, review R1).
	t.Run("uniq with a second operand", func(t *testing.T) {
		t.Parallel()

		for _, args := range [][]string{
			{"uniq", "/srv/audit/work/x", "/srv/audit/state/probe"},
			{"uniq", "-c", "/srv/audit/work/x", "/srv/audit/state/probe"},
			{"uniq", "-", "/srv/audit/state/probe"},
		} {
			code, stderr, recorder := runWrapper(t, args)

			if code != refusedExit {
				t.Errorf("run(%q) = %d, want %d: uniq's second operand is a file it writes", args, code, refusedExit)
			}

			if recorder.called {
				t.Errorf("run(%q) exec'd %q; a refused line runs nothing", args, recorder.call.argv)
			}

			if !strings.Contains(stderr, "/srv/audit/state/probe") {
				t.Errorf("run(%q) wrote %q to stderr, want it to name the refused operand", args, stderr)
			}
		}
	})
}

// TestRunHoldsEveryPathWordToTheAllowList pins the wrapper's own path rule (issue
// #277, rounds F and G): Claude Code's deny rules match the command text, so a
// glob such as `audit-exec cat /proc/[0-9]*/envir?n` gets past them and the
// wrapper then receives real paths. The wrapper holds every path word of a line
// to an allow list instead: the three roots the audit works in.
//
// Round F refused a deny list of roots - /proc, /sys and /dev - and round G's
// re-check found six ways past it: a "-"-led operand after "--",
// /dev/fd/../../1/environ (the kernel follows /dev/fd into /proc before the
// ".."), an attached short-option value (git grep -f/sys/x), the roots /home,
// /etc and the state area, git diff --no-index, and the caller's working
// directory. The rule, pinned here:
//
//   - a word is a path word when it is a non-option word holding a "/" (a word
//     with no "/" is a name in the working directory, and "-" alone is stdin);
//     any word after "--", whatever its first character; an option's value
//     after its first "="; and, in every single-dash word that holds a "/",
//     the suffix from its first "/" (git blame -wS/var/lib/dpkg/status, git
//     grep -n5f/tmp/x: round K stopped modelling each tool's cluster parser,
//     so the value's own suffix must be an allowed absolute path whatever
//     letter, digit or dash the value is attached after). git's -C is no path
//     word of this rule: a caller's -C is refused before the rule reads the
//     line (round H).
//   - an absolute path word is read as it is, a relative one on a git line is
//     joined to /srv/audit/checkout - which is what git reads it under the
//     wrapper's own -C - and a relative one on any other line to
//     /srv/audit/work, the wrapper's working directory;
//   - a word holding a ".." element is refused before it is cleaned, whatever
//     it resolves to, so /dev/fd/../../1/environ, which the kernel resolves
//     through /proc/self/fd, is no way through; every other word is refused
//     unless its cleaned path is /srv/audit/checkout, /srv/audit/checks or
//     /srv/audit/work, or lies under one of them.
//
// The rule fails closed, which the ADR accepts: a git ref such as origin/main
// resolves under the checkout and passes, a git grep pattern holding an
// absolute path outside the three roots (-e /proc/) is refused, and a URL-like
// pattern (http://x) cleans to a relative name and passes.
func TestRunHoldsEveryPathWordToTheAllowList(t *testing.T) {
	t.Parallel()

	t.Run("a path word outside the three roots is refused", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name      string
			args      []string
			wantNamed string
		}{
			// Round G's bypasses: each one got past round F's deny list of roots.
			{
				name:      "a -led operand after --",
				args:      []string{"cat", "--", "-/../../proc/1/environ"},
				wantNamed: "-/../../proc/1/environ",
			},
			{
				// /dev/fd is a link to /proc/self/fd, which the kernel follows
				// before it takes the "..": the word is refused for its "..",
				// before anything is cleaned.
				name:      "a path through /dev/fd and its ..",
				args:      []string{"cat", "/dev/fd/../../1/environ"},
				wantNamed: "/dev/fd/../../1/environ",
			},
			{
				name:      "a path through /dev/stdin and its ..",
				args:      []string{"cat", "/dev/stdin/../../environ"},
				wantNamed: "/dev/stdin/../../environ",
			},
			{
				// The rest of a single-dash word after its first letter is a
				// path word: git grep's -f names the file its patterns are read
				// from.
				name:      "the file of git grep's -f, attached",
				args:      []string{"git", "grep", "-f/sys/x"},
				wantNamed: "-f/sys/x",
			},
			{
				name:      "the value of git blame's -S, attached",
				args:      []string{"git", "blame", "-S/dev/null", "x"},
				wantNamed: "-S/dev/null",
			},
			{
				// jq's -L is the directory it looks for its modules in.
				name:      "the directory of jq's -L, attached",
				args:      []string{"jq", "-L/proc/1/root", "x"},
				wantNamed: "-L/proc/1/root",
			},

			// Round J's bypass: git reads a cluster of short options one letter
			// at a time and gives the letter that takes a value the rest of the
			// word, wherever in the cluster that letter sits. Round I read the
			// rest after the word's first letter alone, so a value attached to a
			// later letter - a path, which git reads and echoes through its
			// error output - got past the rule. Round J read every rest after a
			// leading option letter; round K reads the suffix from the word's
			// first "/" instead (the rows below and the round K ones after
			// them), so these rows are refused for the path their value starts
			// with, no longer for the cluster position that value sits at.
			{
				name:      "the value of git blame's -S, in a cluster of two",
				args:      []string{"git", "blame", "-wS/var/lib/dpkg/status", "HEAD"},
				wantNamed: "-wS/var/lib/dpkg/status",
			},
			{
				name:      "the file of git grep's -f, in a cluster of three",
				args:      []string{"git", "grep", "-nif/run/x", "HEAD"},
				wantNamed: "-nif/run/x",
			},
			{
				// git ls-files' -X is --exclude-from, another file of the
				// caller's, attached to the cluster's last letter.
				name:      "the file of git ls-files' -X, in a cluster of two",
				args:      []string{"git", "ls-files", "-oX/home/runner/x"},
				wantNamed: "-oX/home/runner/x",
			},
			{
				// The state area is outside the three roots, and the value is
				// read as the absolute path it is.
				name:      "the value of git blame's -S, a path in the state area",
				args:      []string{"git", "blame", "-wS/srv/audit/state/x", "HEAD"},
				wantNamed: "-wS/srv/audit/state/x",
			},
			{
				name:      "the file of git grep's -f, a path in /tmp",
				args:      []string{"git", "grep", "-nif/tmp/x", "HEAD"},
				wantNamed: "-nif/tmp/x",
			},

			// Round K's bypass: git's parse-options reads a run of digits in a
			// cluster as -NUM and goes on, so in git grep -n5f/tmp/x the f takes
			// /tmp/x - a file git reads and echoes through its regex error - and
			// the letter-run rule stopped at the digit. The rule no longer reads
			// any tool's cluster: in every single-dash word that holds a "/",
			// the suffix from the first "/" is the path word, and it must lie in
			// the roots whatever letter, digit or dash comes before it.
			{
				name:      "the file of git grep's -f, after a -n5",
				args:      []string{"git", "grep", "-n5f/tmp/x", "HEAD"},
				wantNamed: "-n5f/tmp/x",
			},
			{
				name:      "the file of git grep's -f, after a leading -5",
				args:      []string{"git", "grep", "-5f/var/lib/dpkg/status", "HEAD"},
				wantNamed: "-5f/var/lib/dpkg/status",
			},
			{
				name:      "the file of git grep's -f, after a -0",
				args:      []string{"git", "grep", "-0f/run/x", "HEAD"},
				wantNamed: "-0f/run/x",
			},
			{
				name:      "the file of git grep's -f, after -n1 and -i",
				args:      []string{"git", "grep", "-n1if/run/x", "HEAD"},
				wantNamed: "-n1if/run/x",
			},
			{
				// Round J's reason holds here too: no letter of the word before
				// the value is any part of the path.
				name:      "the value of git blame's -S, one letter before it",
				args:      []string{"git", "blame", "-wS/var/x", "HEAD"},
				wantNamed: "-wS/var/x",
			},

			// Round L's bypass: a word starting with "--" was no path word at
			// all, since the rule read a "--" word's only path word as its value
			// after "=". jq's --rawfile takes the word after the variable as the
			// file it reads, so -- followed by a path made the path a word the
			// rule never read: jq -n --rawfile a --/../../../../etc/passwd '$a'
			// printed any file the sandbox let it open. The rule is now about
			// every word of every line: a word whose "/"-separated elements hold
			// ".." is refused, a word starting with "-" that holds ".."
			// anywhere is refused, and a word starting with "--" whose "/" comes
			// before any "=" is a path word whole, joined to the line's base.
			{
				name:      "the file of jq's --rawfile, behind a -- word",
				args:      []string{"jq", "-n", "--rawfile", "a", "--/../../../../etc/passwd", "$a"},
				wantNamed: "--/../../../../etc/passwd",
			},
			{
				name:      "the file of jq's --slurpfile, behind a --x path word",
				args:      []string{"jq", "-n", "--slurpfile", "a", "--x/../../../../etc/passwd", "."},
				wantNamed: "--x/../../../../etc/passwd",
			},
			{
				name:      "the contents file of git blame, behind a -- word",
				args:      []string{"git", "blame", "--contents", "--/../../../etc/passwd", "HEAD", "--", "f"},
				wantNamed: "--/../../../etc/passwd",
			},
			{
				name:      "the file of git grep's -f, behind a -- word",
				args:      []string{"git", "grep", "-f", "--/../../x", "HEAD"},
				wantNamed: "--/../../x",
			},
			{
				name:      "the file of git ls-files' --exclude-from, behind a -- word",
				args:      []string{"git", "ls-files", "--exclude-from", "--/../x"},
				wantNamed: "--/../x",
			},
			{
				// A ".." element is refused in a word of any shape, a value
				// attached to a single dash included: -S.. names the checkout's
				// parent, the whole tree the audit's containment keeps out.
				name:      "git blame's -S with a .. value",
				args:      []string{"git", "blame", "-S..", "HEAD"},
				wantNamed: "-S..",
			},
			{
				name:      "git grep's -f with a .. value",
				args:      []string{"git", "grep", "-f..", "HEAD"},
				wantNamed: "-f..",
			},
			{
				// A relative word with a ".." element reaches out of the work
				// area without any absolute path: the element itself is refused,
				// whatever the word's shape.
				name:      "a relative word with a .. element",
				args:      []string{"cat", "x/../y"},
				wantNamed: "x/../y",
			},
			{
				name:      "a path into the runner's home",
				args:      []string{"cat", "/home/runner/x"},
				wantNamed: "/home/runner/x",
			},
			{
				name:      "a path into /etc",
				args:      []string{"cat", "/etc/passwd"},
				wantNamed: "/etc/passwd",
			},
			{
				// The state area is outside the three roots: the audit's own home
				// and settings live there.
				name:      "a path into the state area's home",
				args:      []string{"cat", "/srv/audit/state/home/.claude.json"},
				wantNamed: "/srv/audit/state/home/.claude.json",
			},
			{
				name:      "a path into /tmp",
				args:      []string{"cat", "/tmp/x"},
				wantNamed: "/tmp/x",
			},
			{
				// A word without a "/" is a name in the working directory, which
				// is /srv/audit/work: ".." is its parent, outside the roots.
				name:      "the work directory's parent",
				args:      []string{"cat", ".."},
				wantNamed: "..",
			},
			{
				// A word after "--" is a path word whatever its first character,
				// so a git pathspec out of the checkout is refused.
				name:      "a pathspec after -- out of the checkout",
				args:      []string{"git", "log", "--", "../../../proc/1/environ"},
				wantNamed: "../../../proc/1/environ",
			},
			{
				// Round F exec'd this line: its ".." stopped short of the root,
				// so it read /srv/proc/1/environ and was no path into /proc. The
				// allow list refuses the ".." itself.
				name:      "a path whose .. stop short of the root",
				args:      []string{"cat", "/srv/audit/work/../../proc/1/environ"},
				wantNamed: "/srv/audit/work/../../proc/1/environ",
			},

			// Round F's rows stay: each names a path outside the three roots.
			{
				name:      "cat of a process's environment",
				args:      []string{"cat", "/proc/1/environ"},
				wantNamed: "/proc/1/environ",
			},
			{
				name:      "head of the wrapper's own environment",
				args:      []string{"head", "/proc/self/environ"},
				wantNamed: "/proc/self/environ",
			},
			{
				name:      "od of a kernel file",
				args:      []string{"od", "/sys/kernel/x"},
				wantNamed: "/sys/kernel/x",
			},
			{
				// The word is refused for its ".." element, before it is
				// cleaned: what it resolves to is not what decides.
				name:      "cat of a path that cleans into /proc",
				args:      []string{"cat", "/proc/../proc/1/environ"},
				wantNamed: "/proc/../proc/1/environ",
			},
			{
				name:      "cat of the directory itself",
				args:      []string{"cat", "/proc"},
				wantNamed: "/proc",
			},
			{
				name:      "tail of a device",
				args:      []string{"tail", "/dev/null"},
				wantNamed: "/dev/null",
			},
			{
				// A relative word on a line that is no git line is joined to the
				// wrapper's working directory, /srv/audit/work, which ../../ leaves:
				// the word is refused for its "..".
				name:      "base64 of a relative path out of the work directory",
				args:      []string{"base64", "../../proc/1/environ"},
				wantNamed: "../../proc/1/environ",
			},
			{
				name:      "a path after an option of cat's",
				args:      []string{"cat", "-n", "/proc/1/environ"},
				wantNamed: "/proc/1/environ",
			},
			{
				// The value of an option, attached after its =, is a path too.
				name:      "the value of an attached option",
				args:      []string{"sort", "--field-separator=/proc/x", "/srv/audit/work/x"},
				wantNamed: "--field-separator=/proc/x",
			},
			{
				// jq reads the file its --rawfile names, after the variable it
				// fills.
				name:      "the file of jq's --rawfile",
				args:      []string{"jq", "--rawfile", "canary", "/proc/self/environ", "."},
				wantNamed: "/proc/self/environ",
			},
			{
				// The CLI's deny rule matched this path as text; the wrapper
				// refuses it for its ".." element, as for every other one.
				name:      "a path built out of the work directory",
				args:      []string{"sha256sum", "/srv/audit/work/../../../proc/1/environ"},
				wantNamed: "/srv/audit/work/../../../proc/1/environ",
			},
			{
				// The wrapper cleans lexically with strings, since depguard keeps
				// path/filepath from it: the rows below pin that its clean agrees
				// with filepath.Clean on the edges, a repeated or trailing slash,
				// a ./ element and a .. above the root, which stays at the root.
				name:      "a path with a doubled leading slash",
				args:      []string{"cat", "//proc/1/environ"},
				wantNamed: "//proc/1/environ",
			},
			{
				name:      "a path with a ./ element",
				args:      []string{"cat", "/./proc/1/environ"},
				wantNamed: "/./proc/1/environ",
			},
			{
				name:      "a path with a trailing slash",
				args:      []string{"cat", "/proc/1/"},
				wantNamed: "/proc/1/",
			},
			{
				name:      "a path whose .. run above the root",
				args:      []string{"cat", "/srv/audit/work/../../../../proc/1/environ"},
				wantNamed: "/srv/audit/work/../../../../proc/1/environ",
			},
			{
				// The rule fails closed: a git grep pattern holding an absolute
				// path outside the three roots is refused, as asksForSignature
				// refuses %G.
				name:      "a grep pattern that reads as a path",
				args:      []string{"git", "grep", "-e", "/proc/1/environ", "/srv/audit/checkout/f"},
				wantNamed: "/proc/1/environ",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				code, stderr, recorder := runWrapper(t, tc.args)

				if code != refusedExit {
					t.Errorf("run(%q) = %d, want %d: the wrapper holds every path word to /srv/audit/checkout, /srv/audit/checks and /srv/audit/work itself, whatever the CLI's deny rule matched (stderr %q)", tc.args, code, refusedExit, stderr)
				}

				if recorder.called {
					t.Errorf("run(%q) exec'd %q; a refused line runs nothing", tc.args, recorder.call.argv)
				}

				if !strings.Contains(stderr, tc.wantNamed) {
					t.Errorf("run(%q) wrote %q to stderr, want it to name the refused word %q", tc.args, stderr, tc.wantNamed)
				}
			})
		}
	})

	t.Run("a line whose path words are inside the roots is exec'd whole", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name     string
			args     []string
			wantPath string
		}{
			{
				// The repository's own dev/ is what its tooling reads: round F
				// refused it, and the allow list does not. A word after "--" is a
				// path word, and a relative one on a git line is read from the
				// checkout, which is what git reads it under the wrapper's -C.
				name:     "git log of the checkout's dev/",
				args:     []string{"git", "log", "--", "dev/tofu.sh"},
				wantPath: "/usr/bin/git",
			},
			{
				name:     "git blame of the checkout's dev/",
				args:     []string{"git", "blame", "dev/lima.yaml"},
				wantPath: "/usr/bin/git",
			},
			{
				name:     "git diff of the checkout's dev/",
				args:     []string{"git", "diff", "HEAD~1", "--", "dev/"},
				wantPath: "/usr/bin/git",
			},
			{
				// A git ref holding a "/" resolves under the checkout, where git
				// reads it: the fail-closed side effect the ADR accepts.
				name:     "a git ref holding a slash",
				args:     []string{"git", "log", "origin/main"},
				wantPath: "/usr/bin/git",
			},
			{
				name:     "cat of the checkout's dev/ by its full path",
				args:     []string{"cat", "/srv/audit/checkout/dev/tofu.sh"},
				wantPath: "/usr/bin/cat",
			},
			{
				// A word without a "/" is a name in the wrapper's working
				// directory, /srv/audit/work.
				name:     "a bare name in the work directory",
				args:     []string{"cat", "x"},
				wantPath: "/usr/bin/cat",
			},
			{
				name:     "cat of the work directory's file",
				args:     []string{"cat", "/srv/audit/work/x"},
				wantPath: "/usr/bin/cat",
			},
			{
				name:     "the word proc in a grep pattern is no path",
				args:     []string{"git", "grep", "-e", "proc", "/srv/audit/checkout/f"},
				wantPath: "/usr/bin/git",
			},
			{
				name:     "a directory named proc inside the work area is no /proc",
				args:     []string{"cat", "/srv/audit/work/proc/x"},
				wantPath: "/usr/bin/cat",
			},
			{
				name:     "a jq filter is no path",
				args:     []string{"jq", "-r", ".findings", "/srv/audit/work/result.json"},
				wantPath: "/usr/bin/jq",
			},
			{
				name:     "stdin is no path",
				args:     []string{"cat", "-"},
				wantPath: "/usr/bin/cat",
			},
			{
				name:     "head's -n value and its file",
				args:     []string{"head", "-n", "5", "/srv/audit/work/x"},
				wantPath: "/usr/bin/head",
			},
			{
				name:     "sort's -k value and its file in the work area",
				args:     []string{"sort", "-k", "2,2", "/srv/audit/work/x"},
				wantPath: "/usr/bin/sort",
			},
			{
				name:     "sort's -k value and its file in the checks area",
				args:     []string{"sort", "-k", "2,2", "/srv/audit/checks/a"},
				wantPath: "/usr/bin/sort",
			},
			{
				name:     "a relative path inside the work directory",
				args:     []string{"cat", "./x"},
				wantPath: "/usr/bin/cat",
			},
			{
				// Round J's other side: only the word's leading option letters
				// start a value, so the letters inside a value are no option
				// letters, and this word's one value lies in the work area.
				name:     "git grep's -f with its value in the work area",
				args:     []string{"git", "grep", "-if/srv/audit/work/p", "HEAD"},
				wantPath: "/usr/bin/git",
			},
			{
				// git blame's -S takes the absolute path its value is, and that
				// path is in the work area.
				name:     "git blame's -S with its value in the work area",
				args:     []string{"git", "blame", "-S/srv/audit/work/revs", "HEAD"},
				wantPath: "/usr/bin/git",
			},
			{
				// Round K's other side: the suffix from the word's first "/" is
				// what the rule reads, so the -NUM form passes when its file lies
				// in the work area.
				name:     "git grep's -f after a -n5, with its value in the work area",
				args:     []string{"git", "grep", "-n5f/srv/audit/work/p", "HEAD"},
				wantPath: "/usr/bin/git",
			},
			{
				// A -NUM takes no value that holds a "/", so the word is no path
				// word at all.
				name:     "git grep's -n5 and its pattern",
				args:     []string{"git", "grep", "-n5", "-e", "x"},
				wantPath: "/usr/bin/git",
			},
			{
				// A pattern and an option hold no path word, so a line whose
				// one non-option word is a pattern reads only.
				name:     "git grep with a pattern and no path",
				args:     []string{"git", "grep", "-n", "-e", "x"},
				wantPath: "/usr/bin/git",
			},

			// Round L's word rule has its other side too: a word that holds a
			// ".." that is no path element, or a "/" that is no part of a path
			// word, stays a word the line reads.
			{
				// A git range holds a ".." but no ".." element: the pair
				// joins two revisions inside one word and names no directory, so
				// the element rule takes the word.
				name:     "a git range",
				args:     []string{"git", "log", "HEAD~3..HEAD"},
				wantPath: "/usr/bin/git",
			},
			{
				name:     "a git range of two remote branches",
				args:     []string{"git", "diff", "main..origin/main"},
				wantPath: "/usr/bin/git",
			},
			{
				// The "/" of this -- word's value comes after its "=", so the
				// word is no path word whole: the path word is the value after
				// the "=", read from the checkout, and the format string is
				// inside it. A rule that took the whole word would read %h/%s
				// from the root and refuse this line.
				name:     "git log's --format value holding a /",
				args:     []string{"git", "log", "--format=%h/%s"},
				wantPath: "/usr/bin/git",
			},
			{
				// A -- word whose "/" comes before its "=" is a path word
				// whole, joined to the line's base: on a jq line that is the
				// wrapper's working directory, so --x/y is /srv/audit/work/--x/y,
				// inside the work area.
				name:     "a jq --rawfile of a --x path word in the work area",
				args:     []string{"jq", "-n", "--rawfile", "a", "--x/y", "$a"},
				wantPath: "/usr/bin/jq",
			},
			{
				name:     "git log's --format value holding no /",
				args:     []string{"git", "log", "--format=%H"},
				wantPath: "/usr/bin/git",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				code, stderr, recorder := runWrapper(t, tc.args)

				if code != 0 {
					t.Fatalf("run(%q) = %d, want 0 (stderr %q): every path word of the line is inside /srv/audit/checkout, /srv/audit/checks or /srv/audit/work", tc.args, code, stderr)
				}

				if !recorder.called {
					t.Fatalf("run(%q) never reached the exec seam: every path word of the line is inside one of the three roots", tc.args)
				}

				if recorder.call.path != tc.wantPath {
					t.Errorf("run(%q) execs %q, want %q", tc.args, recorder.call.path, tc.wantPath)
				}
			})
		}
	})
}

// TestRunTakesTheOptionsThatOnlyRead pins the other half of the allow lists: the
// lines a legitimate command line makes, which the wrapper still execs whole.
// sort's own allow list takes -k and -t with their value attached or as the
// next argument, a cluster of allowed letters, and the long form of every
// allowed short option; git's clusters of allowed letters stay what they were
// (issue #277, review R1).
func TestRunTakesTheOptionsThatOnlyRead(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		args     []string
		wantPath string
	}{
		{
			name:     "sort -k attached and -t attached",
			args:     []string{"sort", "-k2,2n", "-t:", "/srv/audit/work/x"},
			wantPath: "/usr/bin/sort",
		},
		{
			name:     "sort -k and -t with their value as the next argument",
			args:     []string{"sort", "-k", "2,2", "-t", ":", "/srv/audit/work/x"},
			wantPath: "/usr/bin/sort",
		},
		{
			name:     "sort with a cluster of allowed letters",
			args:     []string{"sort", "-bdfg", "/srv/audit/work/x"},
			wantPath: "/usr/bin/sort",
		},
		{
			name:     "sort's whole allow list as short options",
			args:     []string{"sort", "-b", "-d", "-f", "-g", "-h", "-i", "-M", "-n", "-r", "-R", "-s", "-u", "-V", "-z", "/srv/audit/work/x"},
			wantPath: "/usr/bin/sort",
		},
		{
			name:     "sort's allow list in its long forms",
			args:     []string{"sort", "--ignore-leading-blanks", "--dictionary-order", "--ignore-case", "--general-numeric-sort", "--human-numeric-sort", "--ignore-nonprinting", "--month-sort", "--numeric-sort", "--reverse", "--random-sort", "--stable", "--unique", "--version-sort", "--zero-terminated", "/srv/audit/work/x"},
			wantPath: "/usr/bin/sort",
		},
		{
			name:     "sort --key and --field-separator",
			args:     []string{"sort", "--key=1,1", "--field-separator=:", "/srv/audit/work/x"},
			wantPath: "/usr/bin/sort",
		},
		{
			name:     "git grep with two options and a pattern",
			args:     []string{"git", "grep", "-n", "-e", "x"},
			wantPath: "/usr/bin/git",
		},
		{
			name:     "git grep with a cluster of allowed letters",
			args:     []string{"git", "grep", "-in", "x"},
			wantPath: "/usr/bin/git",
		},
		{
			name:     "git log with its own -i and -n",
			args:     []string{"git", "log", "-i", "-n", "20"},
			wantPath: "/usr/bin/git",
		},
		{
			// %H and %n are the hash and a newline: the format placeholders a
			// report's own table is built from, and no %G among them (issue #277,
			// round D finding 2).
			name:     "git log's own format placeholders",
			args:     []string{"git", "log", "--format=%H%n%s"},
			wantPath: "/usr/bin/git",
		},
		{
			// for-each-ref's own sort atoms are no signature one, so the line
			// runs as it is (issue #277, round E finding 2).
			name:     "git for-each-ref's own sort atom",
			args:     []string{"git", "for-each-ref", "--sort=-committerdate"},
			wantPath: "/usr/bin/git",
		},
		{
			// jq's module system is refused (issue #277, round H, finding 2), so
			// a filter that is no module and names no library path runs as it is.
			name:     "jq's own filter over a check's output",
			args:     []string{"jq", ".[] | .id", "/srv/audit/checks/a.json"},
			wantPath: "/usr/bin/jq",
		},
		{
			// jq's program file is refused (issue #277, round I), so a filter
			// on the line over the result file runs as it is.
			name:     "jq's filter over the result file, no program file",
			args:     []string{"jq", "-r", ".findings", "/srv/audit/work/result.json"},
			wantPath: "/usr/bin/jq",
		},
		{
			// wc and sha256sum read their paths from the line: no
			// --files0-from, no --check (issue #277, round I).
			name:     "wc of a file in the work area",
			args:     []string{"wc", "-l", "/srv/audit/work/x"},
			wantPath: "/usr/bin/wc",
		},
		{
			name:     "sha256sum of a file in the work area",
			args:     []string{"sha256sum", "/srv/audit/work/x"},
			wantPath: "/usr/bin/sha256sum",
		},
		{
			name:     "uniq with one operand",
			args:     []string{"uniq", "/srv/audit/work/x"},
			wantPath: "/usr/bin/uniq",
		},
		{
			name:     "uniq with its one option and one operand",
			args:     []string{"uniq", "-c", "/srv/audit/work/x"},
			wantPath: "/usr/bin/uniq",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			code, stderr, recorder := runWrapper(t, tc.args)

			if code != 0 {
				t.Fatalf("run(%q) = %d, want 0 (stderr %q): the line reads only, so the wrapper execs it", tc.args, code, stderr)
			}

			if !recorder.called {
				t.Fatalf("run(%q) never reached the exec seam: the line reads only, so the wrapper execs it", tc.args)
			}

			if recorder.call.path != tc.wantPath {
				t.Errorf("run(%q) execs %q, want %q", tc.args, recorder.call.path, tc.wantPath)
			}
		})
	}
}

// TestRunNamesARefusedWordInOneEscapedLine pins how a refusal reads: one line,
// whatever the refused word holds - a newline, a control character or a %,
// which a workflow command would read - so the line cannot become two or be
// taken for a command - and a word over 64 bytes is cut, so no name a caller
// chose can fill the log.
func TestRunNamesARefusedWordInOneEscapedLine(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		word string
	}{
		{name: "a newline", word: "sh\n::warning::injected"},
		{name: "a carriage return", word: "sh\r::warning::injected"},
		{name: "a percent", word: "sh%0An"},
		{name: "a long name", word: strings.Repeat("a", 200)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			code, stderr, _ := runWrapper(t, []string{tc.word, "x"})

			if code != refusedExit {
				t.Fatalf("run(%q) = %d, want %d", tc.word, code, refusedExit)
			}

			if got := strings.Count(stderr, "\n"); got != 1 {
				t.Errorf("run(%q) wrote %d lines to stderr (%q), want one", tc.word, got, stderr)
			}

			if strings.Contains(stderr, "\r") {
				t.Errorf("run(%q) wrote %q to stderr, want no carriage return", tc.word, stderr)
			}

			for _, line := range strings.Split(stderr, "\n") {
				if strings.HasPrefix(line, "::") {
					t.Errorf("run(%q) wrote %q to stderr, whose line %q is a workflow command a caller's name wrote", tc.word, stderr, line)
				}
			}
		})
	}

	t.Run("a long name is cut", func(t *testing.T) {
		t.Parallel()

		word := strings.Repeat("b", 200)

		_, stderr, _ := runWrapper(t, []string{word, "x"})

		if strings.Contains(stderr, word) {
			t.Errorf("run(%q) wrote %q to stderr, want the refused word cut to 64 bytes", word, stderr)
		}

		if !strings.Contains(stderr, word[:64]) {
			t.Errorf("run(%q) wrote %q to stderr, want the refused word's first 64 bytes", word, stderr)
		}
	})
}

// TestRunChangesIntoTheWorkDirectory pins the wrapper's fixed working directory
// (issue #277, round G): before every exec the wrapper changes into
// /srv/audit/work, so a relative name on a line is a name there whatever
// directory the caller was in - which is where the path rule reads it from - and
// a change that fails ends the run with 127, as a failed exec does. git keeps
// its own -C /srv/audit/checkout, which the exec's arguments still hold.
func TestRunChangesIntoTheWorkDirectory(t *testing.T) {
	t.Parallel()

	t.Run("every exec changes into the work directory first", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name     string
			args     []string
			wantPath string
		}{
			{
				name:     "git log",
				args:     []string{"git", "log", "-1"},
				wantPath: "/usr/bin/git",
			},
			{
				name:     "cat of the work directory's file",
				args:     []string{"cat", "/srv/audit/work/x"},
				wantPath: "/usr/bin/cat",
			},
			{
				name:     "cat of a bare name the work directory holds",
				args:     []string{"cat", "x"},
				wantPath: "/usr/bin/cat",
			},
			{
				name:     "sort",
				args:     []string{"sort", "-u"},
				wantPath: "/usr/bin/sort",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				recorder := &seamRecorder{}

				var stderr bytes.Buffer

				if code := run(tc.args, recorder.chdir, recorder.exec, &stderr); code != 0 {
					t.Fatalf("run(%q) = %d, want 0 (stderr %q): the line is exec'd", tc.args, code, stderr.String())
				}

				want := []string{"chdir " + auditWork, "exec " + tc.wantPath}

				if diff := cmp.Diff(want, recorder.steps); diff != "" {
					t.Errorf("run(%q) took these steps (-want +got):\n%s", tc.args, diff)
				}
			})
		}
	})

	t.Run("a failed change into the work directory runs nothing", func(t *testing.T) {
		t.Parallel()

		recorder := &seamRecorder{chdirErr: dirFailed{}}

		var stderr bytes.Buffer

		if code := run([]string{"cat", "/srv/audit/work/x"}, recorder.chdir, recorder.exec, &stderr); code != failedExit {
			t.Errorf("run([cat]) = %d, want %d: a command the wrapper could not change directory for does not run", code, failedExit)
		}

		if len(recorder.executed) != 0 {
			t.Errorf("run([cat]) exec'd %q after the failed change: a run that could not change directory execs nothing", recorder.executed[0].path)
		}

		if got := strings.Count(stderr.String(), "\n"); got != 1 {
			t.Errorf("run([cat]) wrote %d lines to stderr (%q), want one saying the command could not run", got, stderr.String())
		}
	})

	t.Run("a refused line changes nothing", func(t *testing.T) {
		t.Parallel()

		for _, args := range [][]string{
			{"sh", "-c", "true"},
			{"cat", "/proc/1/environ"},
		} {
			recorder := &seamRecorder{}

			var stderr bytes.Buffer

			if code := run(args, recorder.chdir, recorder.exec, &stderr); code != refusedExit {
				t.Errorf("run(%q) = %d, want %d", args, code, refusedExit)
			}

			if len(recorder.steps) != 0 {
				t.Errorf("run(%q) took the steps %v; a refused line changes no directory and runs nothing", args, recorder.steps)
			}
		}
	})
}

// TestRunReportsAFailedExec pins that a command the seam could not start is no
// success: the wrapper does not exit 0, and says on stderr which command failed.
func TestRunReportsAFailedExec(t *testing.T) {
	t.Parallel()

	recorder := &execRecorder{err: errExecFailed{}}

	var stderr bytes.Buffer

	if code := run([]string{"cat", "/srv/audit/work/x"}, func(string) error { return nil }, recorder.exec, &stderr); code == 0 {
		t.Errorf("run([cat]) = 0, want a failure status: the exec seam could not start the command")
	}

	if !recorder.called {
		t.Fatal("run([cat]) never reached the exec seam")
	}

	if stderr.Len() == 0 {
		t.Error("run([cat]) wrote nothing to stderr, want a line saying the command could not run")
	}
}

// errExecFailed is the failure a seam answers with when the path cannot be run.
type errExecFailed struct{}

// Error implements error.
func (errExecFailed) Error() string { return "no such file or directory" }

// valueArgs returns the arguments an option that takes a value is given, or
// none for a flag that takes no value.
func valueArgs(value string) []string {
	if value == "" {
		return nil
	}

	return []string{value}
}
