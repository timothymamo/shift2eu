package lint_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/timothymamo/shift2eu/test/internal/execenv"
)

// The checker these scans run, and its probes, live in test/internal/execenv
// (issue #195), as the shellcheck rule's do in test/internal/shellcheck:
// test/lint holds doc.go and _test.go files only.

// e2eToolchainCommands lists the commands a test/e2e _test.go file may start
// with the whole environment of the test process, as the name and the
// subcommand the source writes, joined by a space. It is the go toolchain's
// alone: the `go build` calls of assess_test.go's buildAndRun and the
// `go list -deps` calls of assess_test.go inherit that
// environment because the go command needs it (GOPATH, the module cache,
// GOFLAGS, a proxy, a toolchain) and print none of it (issue #195). Every other
// command must set its Env: exec.Cmd reads a nil Env as "inherit the
// environment", where the agents' keys and tokens live (CLAUDE.md rule 3), and
// what the child prints can reach a t.Log call or a golden file. The exemption
// is keyed on the name and the subcommand, both string literals, so a `go run`
// or a `go env` call is let through by nothing; the checker itself takes the
// exemption away from a call with a spread argument (issue #195 round 4). A
// resulting argument the source builds at run time (an output path, a package)
// keeps the exemption, so an argument that holds a flag without being a literal
// is left to review, as the e2e-no-env forbidigo group leaves its own gaps. A Dir
// set on an exempt command keeps the exemption too: the go command then reads
// that directory's go.mod and go.work, and that is left to review, like a
// non-literal argument holding a flag.
var e2eToolchainCommands = []string{"go build", "go list"}

// e2eToolchainFlags is the allow-list of flags an exempt command may carry:
// after the subcommand, every literal argument that starts with "-" must be one
// of these, or the exemption is lost (issue #195 round 4). The four are the
// flags the tier passes today, and each keeps the go command from handing its
// environment to a program the argument names: -trimpath and -o build a binary,
// -deps lists the packages it needs, and -ldflags (e2eLinkerFlag) stamps a
// package's variable at link time, admitted for the one value e2eLinkerStamps
// describes. A flag the go command gains later, or one of the many that run
// another program with that environment (-toolexec, -exec, -overlay, -C, -pgo,
// -compiler, ...), is refused until review adds it here on purpose.
var e2eToolchainFlags = []string{"-trimpath", "-o", "-deps", "-ldflags"}

// e2eLinkerFlag is the go build flag that stamps a package's variable at link
// time, which the e2e tier passes for the version of a second binary of
// ./cmd/shift2eu (issue #584). The flag alone is not enough to admit: its value
// can name another linker, a wrapper around the toolchain or a file the build
// reads (-extld, -toolexec, -overlay, ...), which run a program with the go
// command's environment, and the exemption exists because none of it may reach
// a child of a test. So only a value e2eLinkerStamps describes keeps it.
const e2eLinkerFlag = "-ldflags"

// e2eLinkerStamp is the one form the name=value of a -X stamp may take: an
// import path, a dot, a symbol, exactly one = and a plain value.
var e2eLinkerStamp = regexp.MustCompile(`^[A-Za-z0-9_./-]+\.[A-Za-z_][A-Za-z0-9_]*=[A-Za-z0-9_.:+-]*$`)

// e2eLinkerStamps reports whether value is a -ldflags value the e2e tier may
// carry: pairs of -X and the name=value it stamps, separated by ASCII spaces
// and tabs, and nothing else, so the linker runs no other program and reads no
// other file. cmd/go splits -ldflags with cmd/internal/quoted.Split, which
// differs from a plain split on whitespace in two ways, and a value that could
// tell them apart is refused outright, so this check reads every value it
// admits exactly as cmd/go does:
//   - a quote (' or ") at the start of a field opens a field that runs to the
//     matching quote, spaces included, and ends there, so the text right after
//     the closing quote starts a new field: 'main.a=b'-extld=x is two
//     arguments, and one quoted span can carry a second linker flag (-extld,
//     which runs a named program with the go command's environment). A quote
//     further inside a field is an ordinary byte, and so is a backslash, which
//     quoted.Split never unescapes; both are refused anyway, so no reading of
//     either can matter;
//   - quoted.Split separates fields only on space, tab, \n and \r, fewer
//     characters than strings.Fields, which also splits on \v, \f, U+0085,
//     U+00A0 and the rest of Unicode's white space. White space other than a
//     space or a tab is refused, \n and \r too, which quoted.Split would split
//     on: a stricter choice, as a stamp set on one line is all the e2e tier
//     writes. Today the odd-field rule and e2eLinkerStamp would refuse such a
//     value already; the guard stays on purpose, so the safety does not depend
//     on that regexp staying strict.
//
// So the value holds no quote, no backslash and no white space other than a
// space or a tab. Each name=value field must match e2eLinkerStamp and may not
// start with "-", and each field before it must be exactly -X.
func e2eLinkerStamps(value string) bool {
	if strings.ContainsAny(value, `'"\`) {
		return false
	}

	if strings.ContainsFunc(value, func(r rune) bool { return unicode.IsSpace(r) && r != ' ' && r != '\t' }) {
		return false
	}

	fields := strings.FieldsFunc(value, func(r rune) bool { return r == ' ' || r == '\t' })
	if len(fields) == 0 || len(fields)%2 != 0 {
		return false
	}

	for i, field := range fields {
		if i%2 == 0 {
			if field != "-X" {
				return false
			}

			continue
		}

		if strings.HasPrefix(field, "-") || !e2eLinkerStamp.MatchString(field) {
			return false
		}
	}

	return true
}

// e2eExecExempt reports whether the e2e tier may start the command named name
// with its first argument sub, no Env of its own and the literal arguments
// flags after the subcommand (e2eToolchainCommands, e2eToolchainFlags).
func e2eExecExempt(name, sub string, flags []string) bool {
	if !slices.Contains(e2eToolchainCommands, name+" "+sub) {
		return false
	}

	for i := 0; i < len(flags); i++ {
		flag := flags[i]

		if flag == e2eLinkerFlag {
			// The next literal the source writes is taken as the value. One the
			// source builds at run time is no literal and never reaches this
			// check (the collector drops non-literals), so the next literal
			// after it is read instead: the exemption is lost when that literal
			// is no stamp set (`"-ldflags", v, "-o"`), but kept when it is one
			// (`"-ldflags", v, "-X a.b=c"`). That is the accepted non-literal
			// gap (test/internal/execenv's doc.go), held by review, and harmless
			// today: cmd/go then reads "-X a.b=c" as an unknown flag and fails
			// at flag parsing before it runs any tool.
			if i+1 >= len(flags) || !e2eLinkerStamps(flags[i+1]) {
				return false
			}

			i++

			continue
		}

		if strings.HasPrefix(flag, "-") && !slices.Contains(e2eToolchainFlags, flag) {
			return false
		}
	}
	return true
}

// TestE2EExecExempt pins the exemption the e2e scan hands the checker: the go
// toolchain's two commands, keyed on the name and the subcommand, and only
// while every literal argument after the subcommand that starts with "-" is in
// e2eToolchainFlags. The scan of test/e2e passes none of the flags that spoil
// it today, so only a test of the function itself pins that it refuses them
// (issue #195 round 4).
func TestE2EExecExempt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		cmd   string
		sub   string
		flags []string
		want  bool
	}{
		{
			name:  "the flags buildAndRun passes to go build",
			cmd:   "go",
			sub:   "build",
			flags: []string{"-trimpath", "-o"},
			want:  true,
		},
		{
			name:  "the flags the dependency scans pass to go list",
			cmd:   "go",
			sub:   "list",
			flags: []string{"-deps"},
			want:  true,
		},
		{
			// The stamp of the e2e tier's second binary (issue #584): pairs of -X
			// and a name=value run no program and read no file.
			name: "the -ldflags that stamps a build's version keeps the exemption",
			cmd:  "go",
			sub:  "build",
			flags: []string{
				"-trimpath", "-o", "-ldflags",
				"-X example.com/pkg.version=9.9.9-e2e -X example.com/pkg.commit=abc123def456",
			},
			want: true,
		},
		{
			// The stamp the e2e tier's second binary carries (issue #584): the
			// three internal/version variables, with the import paths and the
			// date a real link line holds.
			name: "the -ldflags the e2e tier stamps its binary with keeps the exemption",
			cmd:  "go",
			sub:  "build",
			flags: []string{
				"-trimpath", "-o", "-ldflags",
				"-X github.com/timothymamo/shift2eu/internal/version.version=9.9.9-e2e " +
					"-X github.com/timothymamo/shift2eu/internal/version.commit=abc123def456 " +
					"-X github.com/timothymamo/shift2eu/internal/version.date=2026-01-01T00:00:00Z",
			},
			want: true,
		},
		{
			// The value can name another linker, a wrapper around the toolchain or
			// a file the build reads, which is why the flag is admitted for -X
			// stamps alone.
			name:  "an -ldflags value that names another flag takes the exemption away",
			cmd:   "go",
			sub:   "build",
			flags: []string{"-ldflags", "-X example.com/pkg.version=9.9.9-e2e -toolexec /bin/sh"},
		},
		{
			name:  "an -ldflags with no value takes the exemption away",
			cmd:   "go",
			sub:   "build",
			flags: []string{"-ldflags"},
		},
		{
			// A value the source builds at run time is no literal, so the next
			// literal argument is the one after it; here it is no stamp set. Had
			// it been one, the exemption would stay: the accepted non-literal gap.
			name:  "an -ldflags whose run-time value is followed by a literal that is no stamp set takes the exemption away",
			cmd:   "go",
			sub:   "build",
			flags: []string{"-ldflags", "-o"},
		},
		{
			name:  "an -ldflags value whose -X names no value takes the exemption away",
			cmd:   "go",
			sub:   "build",
			flags: []string{"-ldflags", "-X example.com/pkg.version"},
		},
		// The go command does not split the value on whitespace alone: cmd/go
		// splits it with cmd/internal/quoted.Split, where a quote opens a field
		// that ends at the closing quote even mid-field, and a backslash escapes
		// the next byte. A field the value writes as one word can therefore carry
		// a second flag, so a value holding a quote or a backslash is no stamp set
		// and the exemption is lost (issue #195).
		{
			name: "an -ldflags value with a single-quoted field that carries another flag takes the exemption away",
			cmd:  "go",
			sub:  "build",
			flags: []string{
				"-ldflags",
				"-X 'main.a=b'-linkmode=external -X 'main.c=d'-extld=/bin/false",
			},
		},
		{
			name: "an -ldflags value with a double-quoted field that carries another flag takes the exemption away",
			cmd:  "go",
			sub:  "build",
			flags: []string{
				"-ldflags",
				`-X "main.a=b"-linkmode=external -X "main.c=d"-extld=/bin/false`,
			},
		},
		{
			// quoted.Split reads a backslash as an ordinary byte, with no
			// unescaping; the checker refuses it anyway, so its reading never
			// matters.
			name:  "an -ldflags value with a backslash takes the exemption away",
			cmd:   "go",
			sub:   "build",
			flags: []string{"-ldflags", `-X main.a=b\c`},
		},
		// The two splits differ on the other whitespace: cmd/internal/quoted
		// splits -ldflags on the ASCII space, tab, newline and carriage return
		// alone, while strings.Fields splits on every Unicode space. A
		// vertical tab, a form feed, a next line or a no-break space therefore
		// keeps the value one field for cmd/go while strings.Fields reads two
		// stamps, so the checker must refuse all of them and read the value as
		// cmd/go does: a stamp set carries the ASCII space and tab alone
		// (issue #195).
		{
			name:  "an -ldflags value that separates two stamps with a vertical tab takes the exemption away",
			cmd:   "go",
			sub:   "build",
			flags: []string{"-ldflags", "-X main.a=b\v-X main.c=d"},
		},
		{
			name:  "an -ldflags value that separates two stamps with a form feed takes the exemption away",
			cmd:   "go",
			sub:   "build",
			flags: []string{"-ldflags", "-X main.a=b\f-X main.c=d"},
		},
		{
			// U+0085 NEXT LINE (NEL) is whitespace to unicode.IsSpace and
			// strings.Fields, but no field separator to cmd/internal/quoted.
			name:  "an -ldflags value that separates two stamps with a next line takes the exemption away",
			cmd:   "go",
			sub:   "build",
			flags: []string{"-ldflags", "-X main.a=b\u0085-X main.c=d"},
		},
		{
			// U+00A0 NO-BREAK SPACE is whitespace to unicode.IsSpace and
			// strings.Fields, but no field separator to cmd/internal/quoted.
			name:  "an -ldflags value that separates two stamps with a no-break space takes the exemption away",
			cmd:   "go",
			sub:   "build",
			flags: []string{"-ldflags", "-X main.a=b\u00a0-X main.c=d"},
		},
		{
			// cmd/go and strings.Fields agree on a newline, but a stamp set is
			// one line of ASCII spaces and tabs; refusing the rest is stricter
			// and simpler than following each split.
			name:  "an -ldflags value that separates two stamps with a newline takes the exemption away",
			cmd:   "go",
			sub:   "build",
			flags: []string{"-ldflags", "-X main.a=b\n-X main.c=d"},
		},
		{
			// A carriage return is refused as well; the odd-field rule refuses
			// it too, but this pins the refusal the comment above documents, not
			// the mechanism that produces it.
			name:  "an -ldflags value that separates two stamps with a carriage return takes the exemption away",
			cmd:   "go",
			sub:   "build",
			flags: []string{"-ldflags", "-X main.a=b\r-X main.c=d"},
		},
		{
			// A stamp is an import path and a symbol, one = and a plain value: a
			// second = is no value the linker takes as this pair.
			name:  "an -ldflags value whose -X names a second = takes the exemption away",
			cmd:   "go",
			sub:   "build",
			flags: []string{"-ldflags", "-X main.a=b=c"},
		},
		{
			// -X names a symbol of a package, which an import path and a name
			// separated by a dot give; a bare name is none.
			name:  "an -ldflags value whose -X names no dotted symbol takes the exemption away",
			cmd:   "go",
			sub:   "build",
			flags: []string{"-ldflags", "-X a=b"},
		},
		{
			name:  "an -ldflags written with = takes the exemption away",
			cmd:   "go",
			sub:   "build",
			flags: []string{"-ldflags=-X example.com/pkg.version=9.9.9-e2e"},
		},
		{
			name:  "a positional argument the source builds at run time is no flag",
			cmd:   "go",
			sub:   "list",
			flags: []string{"-deps", "./cmd/shift2eu"},
			want:  true,
		},
		{
			// `go run` builds and runs a program with the go command's
			// environment, which is why the exemption names build and list
			// alone.
			name: "a subcommand the exemption does not name",
			cmd:  "go",
			sub:  "run",
		},
		{
			// `go env` prints the environment it inherits.
			name: "go env",
			cmd:  "go",
			sub:  "env",
		},
		{
			name: "another command named with an exempt subcommand",
			cmd:  "sh",
			sub:  "build",
		},
		{
			name:  "a literal flag the allow-list does not name",
			cmd:   "go",
			sub:   "build",
			flags: []string{"-toolexec"},
		},
		{
			name:  "a flag with = the allow-list does not name",
			cmd:   "go",
			sub:   "build",
			flags: []string{"-pgo=auto"},
		},
		{
			// The allow-list is read after the subcommand, so an allowed
			// flag does not rescue a spoil flag further along the call.
			name:  "a spoil flag after an allowed one",
			cmd:   "go",
			sub:   "build",
			flags: []string{"-trimpath", "-overlay", "overlay.json"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := e2eExecExempt(tc.cmd, tc.sub, tc.flags); got != tc.want {
				t.Errorf("e2eExecExempt(%q, %q, %q) = %v, want %v", tc.cmd, tc.sub, tc.flags, got, tc.want)
			}
		})
	}
}

// TestE2ETestsSetExecEnv holds every test/e2e _test.go file to the rule that a
// command it runs carries an Env of its own. The tier imports os/exec on
// purpose, so forbidigo's e2e-no-env group cannot refuse the import; it refuses
// (*exec.Cmd).Environ, the one call that dumps the environment, but nothing
// refuses a command built with a nil Env, which inherits all of it. This test
// keeps every run of the binary setting cmd.Env (issue #195).
// execenv.CheckExecEnv is the checker; the go toolchain's calls are exempt
// (e2eToolchainCommands).
//
// The walk is recursive, so a sub-package the tier grows later is scanned too,
// and skips the testdata directories, which hold fixtures and no package of the
// tier.
func TestE2ETestsSetExecEnv(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)
	dir := filepath.Join(root, "test", "e2e")

	var scanned []string
	refused, err := walkGoFiles(dir, skipTestdataDir, func(rel string, src []byte) error {
		// Every _test.go file of the tier: doc.go is the package's only
		// production file, and TestE2ETestsSetExecEnv holds the tests.
		if !strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		at := "test/e2e/" + rel
		scanned = append(scanned, at)
		findings, err := execenv.CheckExecEnv(at, src, e2eExecExempt)
		if err != nil {
			return fmt.Errorf("check %s: %w", at, err)
		}
		for _, f := range findings {
			t.Error(findingLine(f))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	for _, problem := range refused {
		t.Error(problem)
	}

	// Guard the scan itself: a walk that found no test file, or one that lost
	// the files the toolchain calls live in, would pass whatever they say, and
	// one that descended into a testdata directory would read fixtures as if
	// they were the tier's.
	for _, rel := range []string{
		"test/e2e/assess_test.go",
		"test/e2e/inventory_guide_test.go",
	} {
		if !slices.Contains(scanned, rel) {
			t.Errorf("the scan skipped %s, a _test.go file of the e2e tier", rel)
		}
	}
	for _, at := range scanned {
		if strings.Contains(at, "/testdata/") {
			t.Errorf("the scan read %s, a file under testdata, which holds fixtures and no package of the e2e tier", at)
		}
	}
	t.Logf("checked %d e2e test files", len(scanned))
}

// skipTestdataDir is walkGoFiles' skip function for the e2e tier: a testdata
// directory holds fixtures, which are no Go file of the tier the rule is about.
func skipTestdataDir(_ *os.Root, _, name string) (bool, error) {
	return name == "testdata", nil
}

// TestTestTreeNamesNoProcEnviron holds every Go file under test/ to the rule
// that no string literal in it names the environment of a process: reading that
// file hands over the whole environment of the test process without starting a
// child, and the e2e-no-env forbidigo group allows os.ReadFile, so nothing else
// refuses it (issue #195).
//
// The tier decides the rule (execenv.Tier): under test/e2e, which holds no
// fixture literals, any literal that names a path into /proc or /dev/fd is
// refused; elsewhere in test/, where a probe's source and expected message are
// literals of their own, only a literal whose whole value names a
// /proc/<something>/environ file is.
//
// The walk skips nothing, testdata directories included: a literal there would
// be a probe of some other rule, and the probes of this one are .go.txt files
// under test/internal/execenv/testdata, which are no Go files and which this
// scan therefore never meets. No name is left out: the cases of the matcher
// both tiers read are values in a JSON file there, so the file that pins them,
// test/internal/execenv/environmentpath_test.go, is scanned like every other
// Go file under test/ (issue #195 round 3).
func TestTestTreeNamesNoProcEnviron(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)

	var scanned []string
	refused, err := walkGoFiles(filepath.Join(root, "test"), nil, func(rel string, src []byte) error {
		scanned = append(scanned, rel)
		tier := execenv.TierTest
		if strings.HasPrefix(rel, "e2e/") {
			tier = execenv.TierE2E
		}
		findings, err := execenv.CheckEnvironLiterals("test/"+rel, src, tier)
		if err != nil {
			return fmt.Errorf("check %s: %w", rel, err)
		}
		for _, f := range findings {
			t.Error(findingLine(f))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk test/: %v", err)
	}
	// walkGoFiles refuses a Go file that is not a regular file (whyNotRegular);
	// an unread file would hide the literals inside it.
	for _, problem := range refused {
		t.Error(problem)
	}

	// Guard the walk itself: it must reach the tier this rule is about, a
	// testdata directory, which it does not skip, and the file that pins the
	// matcher on values in a JSON file beside the probes.
	for _, rel := range []string{
		"e2e/assess_test.go",
		"lint/doc.go",
		"lint/testdata/e2enoenv/mod/test/e2e/probe.go",
		"internal/execenv/environmentpath_test.go",
	} {
		if !slices.Contains(scanned, rel) {
			t.Errorf("the scan skipped %s; it reads every Go file under test/", rel)
		}
	}
	t.Logf("checked %d Go files under test/", len(scanned))
}

// findingLine renders one refusal of a checker as the line a reader sees in a
// test failure.
func findingLine(f execenv.Finding) string {
	return fmt.Sprintf("%s:%d: %s", f.File, f.Line, f.Msg)
}
