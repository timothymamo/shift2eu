// Pins the command wrapper the audit's model runs through, audit-exec (ADR
// 0040's milestone-2 amendment, issue #277). The wrapper maps a command name to
// one fixed absolute path and execs it, refuses every other name, refuses git
// outside the subcommands and options the ADR lists, and execs with the scrubbed
// environment the ADR fixes: the caller's environment reaches the command in no
// way.
//
// The tests are white-box (package main): ci/auditexec is a main package, so
// only its own test files reach it, and the exec seam it takes is what a test
// hands its fake, as ci/claudecodepin's entry point does. Nothing is executed:
// the seam records the path, the arguments and the environment the wrapper
// would have replaced itself with.
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

	// refusedExit is the status the wrapper ends with for a refused command
	// name, a refused git subcommand and a refused option alike.
	refusedExit = 126
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

// runWrapper runs the wrapper's logic with args and the fake seam, and returns
// its exit status and its stderr.
func runWrapper(t *testing.T, args []string) (int, string, *execRecorder) {
	t.Helper()

	recorder := &execRecorder{}

	var stderr bytes.Buffer

	code := run(args, recorder.exec, &stderr)

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
		{"--ext-diff", ""},
		{"--textconv", ""},
		{"--output", "/srv/audit/state/probe"},
		// Verifying a signature runs gpg, the program gpg.program names, and
		// git reads a long option's unique prefix as the option itself (issue
		// #277, round D finding 2).
		{"--show-signature", ""},
		{"--show-sig", ""},
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

// TestRunRefusesThePathsIntoProcSysAndDev pins the wrapper's own path rule (issue
// #277, round F finding 2): Claude Code's deny rule Bash(*/proc/*/environ*)
// matches the command text, so a glob such as `audit-exec cat /proc/[0-9]*/envir?n`
// gets past it and the wrapper then receives real paths. The wrapper refuses them
// itself.
//
// The rule, pinned here: a word names a path when it is
//
//   - a word that is no option - no leading "-", or "-" alone, which is stdin -
//     and holds a "/": a bare word such as a grep pattern, a ref or a jq filter
//     names no file, and a path without a "/" is no path into one of the three;
//   - the value of an option, attached after "=" (--field-separator=/proc/x);
//   - git's own -C, attached (-C/proc/1) or as the next word.
//
// A path is refused when filepath.Clean of it, read from "/" when it is
// relative, is /proc, /sys or /dev exactly or lies under one of them, so a
// relative path that reaches one of them from the work directory
// (../../proc/1/environ) is refused as an absolute one is. The rule fails
// closed: a grep pattern that reads as such a path is refused too, as
// asksForSignature refuses a word holding %G whatever option it belongs to.
func TestRunRefusesThePathsIntoProcSysAndDev(t *testing.T) {
	t.Parallel()

	t.Run("a path into one of the three is refused", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name      string
			args      []string
			wantNamed string
		}{
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
				// A path that reaches /proc through its own .. is one of the
				// three once it is cleaned.
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
				// The work directory's parent is /srv/audit, so ../../proc names
				// /proc: a relative path is read as if the working directory
				// were /.
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
				// git -C with its value as the next word.
				name:      "the argument of git's own -C",
				args:      []string{"git", "-C", "/proc/1/environ", "log"},
				wantNamed: "/proc/1/environ",
			},
			{
				// git -C with its value attached to the letter.
				name:      "the argument attached to git's own -C",
				args:      []string{"git", "-C/proc/1", "log"},
				wantNamed: "-C/proc/1",
			},
			{
				// The work directory is /srv/audit/work, so this path is
				// /proc/1/environ and the wrapper refuses what the CLI's deny
				// rule matched as text.
				name:      "a path built out of the work directory",
				args:      []string{"sha256sum", "/srv/audit/work/../../proc/1/environ"},
				wantNamed: "/srv/audit/work/../../proc/1/environ",
			},
			{
				// The rule fails closed: a git grep pattern that reads as one
				// of the three is refused, as asksForSignature refuses %G.
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
					t.Errorf("run(%q) = %d, want %d: the wrapper refuses a path into /proc, /sys or /dev itself, whatever the CLI's deny rule matched (stderr %q)", tc.args, code, refusedExit, stderr)
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

	t.Run("a line that reads elsewhere is exec'd whole", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name     string
			args     []string
			wantPath string
		}{
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
				name:     "sort's -k value and its file",
				args:     []string{"sort", "-k", "2,2", "/srv/audit/work/x"},
				wantPath: "/usr/bin/sort",
			},
			{
				name:     "a relative path inside the work directory",
				args:     []string{"cat", "./x"},
				wantPath: "/usr/bin/cat",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				code, stderr, recorder := runWrapper(t, tc.args)

				if code != 0 {
					t.Fatalf("run(%q) = %d, want 0 (stderr %q): no word of the line names a path into /proc, /sys or /dev", tc.args, code, stderr)
				}

				if !recorder.called {
					t.Fatalf("run(%q) never reached the exec seam: no word of the line names a path into /proc, /sys or /dev", tc.args)
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

// TestRunReportsAFailedExec pins that a command the seam could not start is no
// success: the wrapper does not exit 0, and says on stderr which command failed.
func TestRunReportsAFailedExec(t *testing.T) {
	t.Parallel()

	recorder := &execRecorder{err: errExecFailed{}}

	var stderr bytes.Buffer

	if code := run([]string{"cat", "/srv/audit/work/x"}, recorder.exec, &stderr); code == 0 {
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
