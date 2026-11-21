package lint_test

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// compositionRootTags are the tags of forbidigo's two composition-root groups
// (issue #111). A composition root is the one place that may do what nothing
// else may: cmd/shift2eu builds the object graph. It may not start a program,
// read a named variable or dump the environment.
//
// Group composition-root-no-exec forbids starting a process (os.StartProcess,
// os/exec, syscall.Exec, syscall.ForkExec, syscall.StartProcess, the windows
// entry points syscall.CreateProcess and syscall.CreateProcessAsUser, and a DLL
// it loads with syscall.LoadDLL or syscall.LoadLibrary), a raw system call that
// starts one just as well (syscall.Syscall, its 6, 9, 12, 15, 18 and N forms,
// syscall.RawSyscall and syscall.RawSyscall6, and the syscall.SYS_* number one
// takes), a pointer taken apart with package unsafe to pass a system call an
// address, and dumping the whole environment (os.Environ, syscall.Environ,
// syscall.GetEnvironmentStrings). It has no exception. Group composition-root-env
// forbids reading a named variable (os.Getenv, os.LookupEnv, syscall.Getenv,
// syscall.GetEnvironmentVariable, os.ExpandEnv), and has no exception either:
// the one it had, os.LookupEnv for the HTTP API's token (ADR 0019), went with
// the API (ADR 0037). Both groups hold for files under cmd/shift2eu/ only,
// which the groups' linters.exclusions.rules say and
// TestCompositionRootExclusions checks.
//
// Every name is a pattern, so a group also holds on the release targets that
// lack a name, such as windows: the probe runs once for the host's platform and
// once for windows, which is where the windows names type-check.
var compositionRootTags = []string{"composition-root-no-exec", "composition-root-env"}

// probeTags are the tags of forbidigo's groups TestCompositionRootForbidigoProbe
// covers: the two composition-root groups and the CLI's own group (issue
// #152), which takes over in internal/cli where the roots' groups stop.
var probeTags = slices.Concat(compositionRootTags, []string{cliNoEnvOrProcessTag})

// TestCompositionRootForbidigoProbe runs forbidigo, with the repository's
// .golangci.yml, on the composition root and a CLI package
// (testdata/compositionroot), twice: once for the host's platform, and once for
// windows, where the windows entry points of syscall type-check. Every function
// the two groups forbid has a line in cmd/shift2eu, over all of that command's
// files, and each must be refused with a msg of the right group. A _test.go
// file of a composition root must be refused too:
// its tests are compiled into the same package and run with the same
// environment. Every call the CLI's own group refuses, in internal/cli, has a
// line there with a want of that group, the CLI's _test.go file included, and
// the calls internal/cli is for — os opening the inventory a user names, reading
// it, creating the report and taking the FIFO's syscall.Mkfifo — must pass: a
// group that lost its path-except scope, or one of the roots' that lost its own
// scope and reached internal/cli, is refused by the probe's strictness, which
// reports every issue the probe does not expect.
//
// A file for one platform builds on that platform only, so each run reads the
// want comments of its own files, and an entry of a group need refuse a line in
// one run only: an entry for the windows names fires in the windows run and one
// for the unix names in the host's, and either counts.
func TestCompositionRootForbidigoProbe(t *testing.T) {
	t.Parallel()

	host := runForbidigoProbe(t, forbidigoProbe{
		dir:             "compositionroot",
		tags:            probeTags,
		files:           probeFilesForHost,
		noEntryCoverage: true,
	})
	windows := runForbidigoProbe(t, forbidigoProbe{
		dir:             "compositionroot",
		tags:            probeTags,
		goos:            "windows",
		goarch:          "amd64",
		packages:        []string{"./cmd/..."},
		files:           probeFilesForWindows,
		noEntryCoverage: true,
	})

	for _, e := range forbidigoEntries(t, moduleRoot(t)) {
		if !slices.ContainsFunc(probeTags, func(tag string) bool { return strings.HasPrefix(e.msg, tag+": ") }) {
			continue
		}
		if !host[e.msg] && !windows[e.msg] {
			t.Errorf(".golangci.yml:%d: no probe line in testdata/compositionroot, for the host's platform or for windows, "+
				"is refused by the entry with msg %q; add a line only it refuses", e.line, e.msg)
		}
	}
}

// probeFilesForHost reports whether the host's run lints the probe file at rel,
// a slash-separated path in the probe module: every file but the ones for
// windows, which name syscall's windows entry points and build there only.
func probeFilesForHost(rel string) bool {
	return !strings.HasSuffix(rel, "_windows.go")
}

// probeFilesForWindows reports whether the windows run lints the probe file at
// rel: the files for windows, and the _test.go files of the commands the run
// lints, which name no platform's function and build for every platform. The
// windows run lints ./cmd/... only, as the files of internal/cli name unix
// functions of syscall (the CLI's test takes
// syscall.Mkfifo), and the wants of a file the run does not lint are no wants of
// it.
func probeFilesForWindows(rel string) bool {
	return strings.HasSuffix(rel, "_windows.go") || strings.HasPrefix(rel, "cmd/") && strings.HasSuffix(rel, "_test.go")
}

// rootName is a name as forbidigo renders it, with the import path its pkg is
// matched against: a function as <package>.<name>, and every use of a package
// in os/exec through that package, whichever of its names it uses.
type rootName struct{ pkg, name string }

// rootRefused is a name a group must refuse, with the tag of the group, which
// starts the msg of the entry that must refuse it. The wording of a msg is not
// pinned here: the tag is the contract, and the probe pins that the entry fires.
type rootRefused struct {
	rootName
	tag string
}

// noExecRefused lists every name the composition-root-no-exec group must refuse.
var noExecRefused = []rootRefused{
	{rootName{"os", "os.StartProcess"}, "composition-root-no-exec: "},
	{rootName{"syscall", "syscall.Exec"}, "composition-root-no-exec: "},
	{rootName{"syscall", "syscall.ForkExec"}, "composition-root-no-exec: "},
	{rootName{"syscall", "syscall.StartProcess"}, "composition-root-no-exec: "},
	{rootName{"os/exec", "exec.Command"}, "composition-root-no-exec: "},
	{rootName{"os/exec", "exec.CommandContext"}, "composition-root-no-exec: "},
	{rootName{"os/exec", "exec.LookPath"}, "composition-root-no-exec: "},
	{rootName{"os/exec", "exec.Cmd"}, "composition-root-no-exec: "},
	{rootName{"os", "os.Environ"}, "composition-root-no-exec: "},
	{rootName{"syscall", "syscall.Environ"}, "composition-root-no-exec: "},
	// The windows entry points: a composition root starts no program there
	// either, and loading a DLL runs its code. Every name here exists in
	// syscall on a release target, which is where the probe lints it: a name
	// no target has, such as syscall.LoadLibraryEx, is no pattern's and no
	// probe's to pin.
	{rootName{"syscall", "syscall.CreateProcess"}, "composition-root-no-exec: "},
	{rootName{"syscall", "syscall.CreateProcessAsUser"}, "composition-root-no-exec: "},
	{rootName{"syscall", "syscall.GetEnvironmentStrings"}, "composition-root-no-exec: "},
	{rootName{"syscall", "syscall.LoadDLL"}, "composition-root-no-exec: "},
	{rootName{"syscall", "syscall.MustLoadDLL"}, "composition-root-no-exec: "},
	{rootName{"syscall", "syscall.NewLazyDLL"}, "composition-root-no-exec: "},
	{rootName{"syscall", "syscall.LoadLibrary"}, "composition-root-no-exec: "},
	{rootName{"syscall", "syscall.GetProcAddress"}, "composition-root-no-exec: "},
	// A raw system call starts a process where the kernel call does, the
	// all-threads forms reach every thread as it does, and the syscall.SYS_*
	// numbers are what an exec call is made of.
	{rootName{"syscall", "syscall.Syscall"}, "composition-root-no-exec: "},
	{rootName{"syscall", "syscall.Syscall6"}, "composition-root-no-exec: "},
	{rootName{"syscall", "syscall.Syscall9"}, "composition-root-no-exec: "},
	{rootName{"syscall", "syscall.Syscall12"}, "composition-root-no-exec: "},
	{rootName{"syscall", "syscall.Syscall15"}, "composition-root-no-exec: "},
	{rootName{"syscall", "syscall.Syscall18"}, "composition-root-no-exec: "},
	{rootName{"syscall", "syscall.SyscallN"}, "composition-root-no-exec: "},
	{rootName{"syscall", "syscall.RawSyscall"}, "composition-root-no-exec: "},
	{rootName{"syscall", "syscall.RawSyscall6"}, "composition-root-no-exec: "},
	{rootName{"syscall", "syscall.AllThreadsSyscall"}, "composition-root-no-exec: "},
	{rootName{"syscall", "syscall.AllThreadsSyscall6"}, "composition-root-no-exec: "},
	{rootName{"syscall", "syscall.SYS_EXECVE"}, "composition-root-no-exec: "},
	{rootName{"syscall", "syscall.SYS_CLONE"}, "composition-root-no-exec: "},
	// Package unsafe takes a pointer apart, which is how a raw system call is
	// handed an address; a composition root has no pointer it needs it for.
	{rootName{"unsafe", "unsafe.Pointer"}, "composition-root-no-exec: "},
	{rootName{"unsafe", "unsafe.Sizeof"}, "composition-root-no-exec: "},
}

// noExecAllowed are names a composition root needs, which no
// composition-root-no-exec entry may refuse: naming the process itself, its
// signals and its descriptors, os.Process, and the names that only extend a
// refused one (os.StartProcesses, syscall.Execve, syscall.Environments,
// syscall.CreateProcessX, syscall.Syscalls, syscall.RawSyscalls, syscall.SYSCTL)
// catch a pattern that lost its $. The named-variable reads belong to the other
// group, and a package named os or exec elsewhere is no composition root's.
var noExecAllowed = []rootName{
	{"os", "os.Exit"},
	{"os", "os.Args"},
	{"os", "os.Stdout"},
	{"os", "os.Stderr"},
	{"os", "os.Interrupt"},
	{"os", "os.Getpid"},
	{"os", "os.Getpagesize"},
	{"os", "os.Process"},
	{"os", "os.FindProcess"},
	{"os", "os.Getenv"},
	{"os", "os.LookupEnv"},
	{"os", "os.ExpandEnv"},
	{"syscall", "syscall.SIGTERM"},
	{"syscall", "syscall.SIGHUP"},
	{"syscall", "syscall.Kill"},
	{"syscall", "syscall.Wait4"},
	{"syscall", "syscall.Getpid"},
	{"syscall", "syscall.GetEnvironmentVariable"},
	{"os", "os.StartProcesses"},
	{"syscall", "syscall.Execve"},
	{"syscall", "syscall.Environments"},
	{"syscall", "syscall.CreateProcessX"},
	{"syscall", "syscall.Syscalls"},
	{"syscall", "syscall.RawSyscalls"},
	{"syscall", "syscall.SYSCTL"},
	{"example.com/otheros", "os.StartProcess"},
	{"example.com/otherexec", "exec.Command"},
}

// envRefused lists every name the composition-root-env group must refuse: every
// read of a named variable, and the four lookups that locate a home, config, cache
// or temp directory from an unnamed one (issue #152, the auditor's finding M-B:
// without them a root could read ~/.aws/credentials with lint green).
var envRefused = []rootRefused{
	{rootName{"os", "os.Getenv"}, "composition-root-env: "},
	{rootName{"os", "os.LookupEnv"}, "composition-root-env: "},
	{rootName{"os", "os.ExpandEnv"}, "composition-root-env: "},
	{rootName{"os", "os.UserHomeDir"}, "composition-root-env: "},
	{rootName{"os", "os.UserConfigDir"}, "composition-root-env: "},
	{rootName{"os", "os.UserCacheDir"}, "composition-root-env: "},
	{rootName{"os", "os.TempDir"}, "composition-root-env: "},
	{rootName{"syscall", "syscall.Getenv"}, "composition-root-env: "},
	{rootName{"syscall", "syscall.GetEnvironmentVariable"}, "composition-root-env: "},
}

// envAllowed are names no composition-root-env entry may refuse: the writes, the
// process names, the whole environment (which the other group refuses, on unix
// as syscall.Environ and on windows as syscall.GetEnvironmentStrings), the names
// that only extend a refused one (os.Getenvs, os.LookupEnvs, os.ExpandEnvs,
// os.UserHomeDirs, os.UserConfigDirs, os.UserCacheDirs, os.TempDirs,
// syscall.Getenvs, syscall.GetEnvironmentVariables) catch a pattern that lost
// its $, and a package named os or syscall elsewhere is no composition root's.
var envAllowed = []rootName{
	{"os", "os.Setenv"},
	{"os", "os.Unsetenv"},
	{"os", "os.Clearenv"},
	{"os", "os.Environ"},
	{"os", "os.Exit"},
	{"os", "os.Getpid"},
	{"os", "os.Getpagesize"},
	{"syscall", "syscall.Setenv"},
	{"syscall", "syscall.Unsetenv"},
	{"syscall", "syscall.Environ"},
	{"syscall", "syscall.GetEnvironmentStrings"},
	{"syscall", "syscall.Getpid"},
	{"os", "os.Getenvs"},
	{"os", "os.LookupEnvs"},
	{"os", "os.ExpandEnvs"},
	{"os", "os.UserHomeDirs"},
	{"os", "os.UserConfigDirs"},
	{"os", "os.UserCacheDirs"},
	{"os", "os.TempDirs"},
	{"syscall", "syscall.Getenvs"},
	{"syscall", "syscall.GetEnvironmentVariables"},
	{"example.com/otheros", "os.Getenv"},
	{"example.com/othersyscall", "syscall.Getenv"},
}

// TestCompositionRootForbidigoPatterns checks the entries of the two
// composition-root groups, read from .golangci.yml, offline and without
// golangci-lint: every name a group must refuse is refused by an entry of it, no
// name it must leave alone is refused by one, and every entry refuses a listed
// name. The probe proves how forbidigo renders a name; this pins the patterns
// and their packages over more names than a probe can hold.
func TestCompositionRootForbidigoPatterns(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)

	tests := []struct {
		name    string
		tag     string
		refused []rootRefused
		allowed []rootName
	}{
		{
			name:    "the no-exec group refuses every way to start a program and to dump the environment",
			tag:     "composition-root-no-exec",
			refused: noExecRefused,
			allowed: noExecAllowed,
		},
		{
			name:    "the env group refuses every way to read one named variable",
			tag:     "composition-root-env",
			refused: envRefused,
			allowed: envAllowed,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			entries := groupEntries(t, root, tc.tag)
			refuses := func(e forbidEntry, n rootName) bool {
				return e.pkg.MatchString(n.pkg) && e.pattern.MatchString(n.name)
			}

			for _, r := range tc.refused {
				var msgs []string
				for _, e := range entries {
					if refuses(e, r.rootName) {
						msgs = append(msgs, e.msg)
					}
				}
				if !slices.ContainsFunc(msgs, func(m string) bool { return strings.HasPrefix(m, r.tag) }) {
					t.Errorf("%s (pkg %q) must be refused by an entry of group %s; refused by %q", r.name, r.pkg, r.tag, msgs)
				}
			}
			for _, n := range tc.allowed {
				for _, e := range entries {
					if refuses(e, n) {
						t.Errorf("%s (pkg %q) must pass a composition root, but the entry with msg %q refuses it", n.name, n.pkg, e.msg)
					}
				}
			}
			for _, e := range entries {
				if !slices.ContainsFunc(tc.refused, func(r rootRefused) bool { return refuses(e, r.rootName) }) {
					t.Errorf("the entry with msg %q refuses no name of the group's list; add the names it refuses", e.msg)
				}
			}
		})
	}
}

// TestCompositionRootExclusions checks the linters.exclusions.rules of the two
// composition-root groups, read from .golangci.yml, offline and without
// golangci-lint. Each rule names forbidigo alone and carries a text in the
// documented form of its group, so it drops one group's issues and no other's.
// Both groups hold for files under cmd/shift2eu/ only, so every file elsewhere
// passes them. Neither group has an exception (compositionRootRuleProblems), so
// no rule drops one entry's issues in a file the group holds.
func TestCompositionRootExclusions(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)
	all := exclusionRules(t, root)
	rules := map[string][]exclusionRule{}
	for _, r := range all {
		if r.linters == nil || !slices.Contains(r.linters, "forbidigo") {
			continue
		}
		tag, ok := groupOfText(t, root, r.fields["text"])
		if !ok || !slices.Contains(compositionRootTags, tag) {
			continue // a rule of another group, or one TestForbidigoExclusionsDropOneGroup reports
		}
		rules[tag] = append(rules[tag], r)
	}

	paths := []struct {
		path string
		in   bool // whether both groups hold for it
	}{
		{"cmd/shift2eu/main.go", true},
		{"cmd/shift2eu/main_test.go", true},
		{"cmd/shift2eu/run.go", true},
		{"internal/cli/cli.go", false},
		{"internal/web/web.go", false},
		{"cmd/shift2eu-web/main.go", false},
		{"internal/adapter/source/aws/aws.go", false},
		{"cmd/shift2eux/main.go", false},
		{"cmd/shift2eu.go", false},
		{"main.go", false},
	}

	for _, tag := range compositionRootTags {
		t.Run(tag, func(t *testing.T) {
			t.Parallel()

			if len(rules[tag]) == 0 {
				t.Fatalf(".golangci.yml holds no linters.exclusions.rules entry of group %s, so the group would hold everywhere", tag)
			}
			for _, problem := range compositionRootRuleProblems(t, root, tag, all) {
				t.Error(problem)
			}
			var scope []exclusionRule // the rules that drop the whole group
			for _, r := range rules[tag] {
				if r.fields["text"] == groupText(tag) {
					scope = append(scope, r)
				}
			}
			if len(scope) == 0 {
				t.Errorf("group %s has no exclusion rule that drops the whole group, so nothing scopes it to the composition roots", tag)
			}

			for _, p := range paths {
				if got := inScope(t, scope, p.path); got != p.in {
					t.Errorf("group %s must hold for %s: %t, and the group's exclusion rules make it %t", tag, p.path, p.in, got)
				}
			}
		})
	}
}

// compositionRootRuleProblems returns what is wrong with rules, the
// linters.exclusions.rules entries of .golangci.yml whose text is a text of the
// composition-root group with tag, one line each:
//   - a rule names forbidigo alone, so it drops no other linter's issues;
//   - it has exactly one of path and path-except;
//   - no rule's text is narrower than the group's whole form: such a rule is an
//     exception, which drops one entry's issues, and neither group has one
//     since the HTTP API's token went with the API (ADR 0037). A narrowed
//     path-except rule in particular passes every check of the group's scope,
//     as only a whole-group rule scopes its group, so it would widen what the
//     group allows unseen.
func compositionRootRuleProblems(t *testing.T, root, tag string, rules []exclusionRule) []string {
	t.Helper()

	var problems []string
	exceptions := 0
	for _, r := range rules {
		if got, ok := groupOfText(t, root, r.fields["text"]); !ok || got != tag {
			continue // a rule of another group, or of no group
		}
		if len(r.linters) != 1 || r.linters[0] != "forbidigo" {
			problems = append(problems, fmt.Sprintf(".golangci.yml:%d: an exclusion rule of group %s names linters %q; give forbidigo a rule of its own", r.line, tag, r.linters))
		}
		_, byPath := r.fields["path"]
		_, byExcept := r.fields["path-except"]
		if byPath == byExcept {
			problems = append(problems, fmt.Sprintf(".golangci.yml:%d: an exclusion rule of group %s must have exactly one of path and path-except, and it has %v", r.line, tag, r.fields))
		}
		if r.fields["text"] != groupText(tag) {
			exceptions++
		}
	}
	if exceptions > 0 {
		problems = append(problems, fmt.Sprintf("group %s has no exception, and %d of its exclusion rules drop one entry's msg instead of the whole group", tag, exceptions))
	}
	return problems
}

// TestCompositionRootRuleProblems pins compositionRootRuleProblems, the seam
// that reads the exclusion rules of the two composition-root groups
// (TestCompositionRootExclusions): the rules of a document no test reads from
// disk fail here, and the failure is the pin. A narrowed rule with a
// path-except in particular passes every check of the group's scope, as only a
// whole-group rule is read for it, so it is pinned here.
func TestCompositionRootRuleProblems(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)
	const tag = "composition-root-env"
	scope := func(tag string) exclusionRule {
		return exclusionRule{
			line:    4,
			fields:  map[string]string{"path-except": `^cmd/shift2eu/`, "text": groupText(tag)},
			linters: []string{"forbidigo"},
		}
	}
	// The msg of the entry of the env group that refuses os.UserHomeDir, and of
	// the first entry of the no-exec group: the exceptions below name a real
	// entry's msg, as a narrowed rule does. Each is the msg of one entry alone,
	// which a narrowed text must be to count as its group's.
	var homeDirMsg, noExecMsg string
	for _, e := range groupEntries(t, root, tag) {
		if e.pkg.MatchString("os") && e.pattern.MatchString("os.UserHomeDir") {
			homeDirMsg = e.msg
		}
	}
	if homeDirMsg == "" {
		t.Fatalf(".golangci.yml holds no entry of group %s that refuses os.UserHomeDir", tag)
	}
	noExecMsg = groupEntries(t, root, "composition-root-no-exec")[0].msg

	tests := []struct {
		name  string
		tag   string
		rules []exclusionRule
		want  []string
	}{
		{
			name:  "the whole group on a path-except scopes it to the composition root",
			tag:   tag,
			rules: []exclusionRule{scope(tag)},
		},
		{
			name: "an exception of the env group on one file is refused",
			tag:  tag,
			rules: []exclusionRule{scope(tag), {
				line:    9,
				fields:  map[string]string{"path": `^cmd/shift2eu/main\.go$`, "text": narrowedTextOf(homeDirMsg)},
				linters: []string{"forbidigo"},
			}},
			want: []string{
				"group composition-root-env has no exception, and 1 of its exclusion rules drop one entry's msg instead of the whole group",
			},
		},
		{
			// The hole this pins: the rule drops the os.UserHomeDir entry in
			// every file but the one it names as path-except, so it widens what
			// the env group allows there and no scope check reads it.
			name: "a narrowed rule with a path-except is refused",
			tag:  tag,
			rules: []exclusionRule{scope(tag), {
				line:    9,
				fields:  map[string]string{"path-except": `^cmd/shift2eu/main\.go$`, "text": narrowedTextOf(homeDirMsg)},
				linters: []string{"forbidigo"},
			}},
			want: []string{
				"group composition-root-env has no exception, and 1 of its exclusion rules drop one entry's msg instead of the whole group",
			},
		},
		{
			name: "an exception of the no-exec group is refused",
			tag:  "composition-root-no-exec",
			rules: []exclusionRule{scope("composition-root-no-exec"), {
				line:    9,
				fields:  map[string]string{"path": `^cmd/shift2eu/main\.go$`, "text": narrowedTextOf(noExecMsg)},
				linters: []string{"forbidigo"},
			}},
			want: []string{
				"group composition-root-no-exec has no exception, and 1 of its exclusion rules drop one entry's msg instead of the whole group",
			},
		},
		{
			name: "a scope rule that names a second linter is refused",
			tag:  tag,
			rules: []exclusionRule{{
				line:    4,
				fields:  map[string]string{"path-except": `^cmd/shift2eu/`, "text": groupText(tag)},
				linters: []string{"forbidigo", "gosec"},
			}},
			want: []string{
				`.golangci.yml:4: an exclusion rule of group composition-root-env names linters ["forbidigo" "gosec"]; give forbidigo a rule of its own`,
			},
		},
		{
			name: "a rule that selects nothing is refused",
			tag:  tag,
			rules: []exclusionRule{{
				line:    4,
				fields:  map[string]string{"text": groupText(tag)},
				linters: []string{"forbidigo"},
			}},
			want: []string{
				`.golangci.yml:4: an exclusion rule of group composition-root-env must have exactly one of path and path-except, and it has map[text:because "composition-root-env: [^"]*"$]`,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := compositionRootRuleProblems(t, root, tt.tag, tt.rules)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("compositionRootRuleProblems() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// inScope reports whether the group whose exclusion rules are rules holds for
// path: golangci-lint drops an issue when a rule's path matches it, or when its
// path-except does not, so the group holds where no rule drops it.
func inScope(t *testing.T, rules []exclusionRule, path string) bool {
	t.Helper()

	for _, r := range rules {
		if p, ok := r.fields["path"]; ok && regexp.MustCompile(p).MatchString(path) {
			return false
		}
		if p, ok := r.fields["path-except"]; ok && !regexp.MustCompile(p).MatchString(path) {
			return false
		}
	}
	return true
}

// issueText is the text golangci-lint reports for a use of name, which the entry
// with msg refuses: what an exclusion rule's text is matched against.
func issueText(name, msg string) string {
	return "use of `" + name + "` forbidden because \"" + msg + "\""
}

// forbidEntry is one forbidigo entry of .golangci.yml, its pattern and pkg
// compiled for matching a rendered name, as TestCloudReadOnlyPatterns and
// TestDomainIsPurePatterns match them.
type forbidEntry struct {
	pattern, pkg *regexp.Regexp
	msg          string
}

// groupEntries returns the entries of the forbidigo group with tag, compiled,
// and fails when the group holds none.
func groupEntries(t *testing.T, root, tag string) []forbidEntry {
	t.Helper()

	var entries []forbidEntry
	for _, e := range forbidigoEntries(t, root) {
		if !strings.HasPrefix(e.msg, tag+": ") {
			continue
		}
		pattern, err := regexp.Compile(e.pattern)
		if err != nil {
			t.Fatalf(".golangci.yml:%d: pattern: %v", e.line, err)
		}
		pkg, err := regexp.Compile(e.pkg)
		if err != nil {
			t.Fatalf(".golangci.yml:%d: pkg: %v", e.line, err)
		}
		entries = append(entries, forbidEntry{pattern: pattern, pkg: pkg, msg: e.msg})
	}
	if len(entries) == 0 {
		t.Fatalf(".golangci.yml holds no %s entry", tag)
	}
	return entries
}
