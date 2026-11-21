// Pins dev/infra-lock-compare.sh (issue #434, finding 2 of the AI review of PR #433): the check CI runs after
// `tofu providers lock` in a scratch copy of infra/site, which compares the lock file it regenerated with the
// one git holds. tofu init accepts the committed file as long as every provider it installs has a hash for the
// platform it runs on, so a lock file that lost another platform's hash (a maintainer's darwin_arm64, say)
// still passes `make infra-check` on the machine that dropped it and fails on the next machine, at init, with
// no review of the change that dropped it. The script therefore compares the whole hash set, not the one
// platform: for every provider block its version, its constraints and the set of its hashes must be equal,
// whatever the order of the blocks and of the hashes.
//
// Every case below is derived from the committed infra/site/.terraform.lock.hcl in t.TempDir(), one
// difference each: an identical copy and a reordered one pass; a dropped h1: hash (one platform), a dropped
// zh: hash (a registry checksum), an added hash, a changed version, changed constraints and a provider block
// the committed file misses are refused. So is a provider block the committed file holds and the regenerated
// one misses, and a hash the committed file lists twice. A refusal prints [lock-hashes] with the provider
// address, and names each missing or extra hash. A usage error (a missing argument, a file that is not there)
// is refused too.
//
// A lock file is input, so its lines reach the CI log: a value the script cannot print safely (a constraints
// or hash value holding ##[, which would forge a GitHub Actions annotation, or any other character outside a
// printable set) is named "an unprintable value" and never echoed - the run's whole output must hold no ##[ -
// and a non-blank, non-comment line that is no lock line at all (two hashes on one line, a provider header
// with a trailing comment) is [lock-hashes] too, with the file and the line number, even when both files hold
// it: tofu init would rewrite that file unseen otherwise.
//
// The cases read and write files only: no tofu, no network, no credentials.
package devenv_test

import (
	"errors"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const (
	// infraLockCompareScript is the comparison of the regenerated lock file with the committed one.
	infraLockCompareScript = "dev/infra-lock-compare.sh"
	// infraLockFile is the lock file git holds, the baseline every case derives from.
	infraLockFile = "infra/site/.terraform.lock.hcl"
	// infraLockProvider is the only provider address infra/ may lock, which a refusal must name.
	infraLockProvider = "registry.opentofu.org/scaleway/scaleway"
	// infraLockTag is the tag of a lock-hash refusal's line, as [<rule>] tags are written elsewhere in dev/.
	infraLockTag = "[lock-hashes]"
	// infraLockUsageWord is the word a usage error's message must hold.
	infraLockUsageWord = "usage"
	// infraLockUnprintableWord is the word a refusal must print instead of a value it cannot echo: a
	// constraints or hash value holding ##[ would forge a GitHub Actions annotation in the CI log.
	infraLockUnprintableWord = "unprintable"
	// infraLockOtherProvider is a provider address the committed file does not lock: a case adds its block to
	// that file, so the comparison must refuse a block only the committed file holds.
	infraLockOtherProvider = "registry.opentofu.org/hashicorp/aws"
)

var (
	// lockHashLine matches one hash line of a lock file: indentation, a quoted h1: or zh: hash and an
	// optional trailing comma, as tofu init writes it.
	lockHashLine = regexp.MustCompile(`^(\s+)"([a-z0-9]+:[A-Za-z0-9+/=]+)",?$`)
	// lockVersionLine matches the version line of a provider block.
	lockVersionLine = regexp.MustCompile(`(?m)^(\s*version\s*=\s*")[^"]*(")$`)
	// lockConstraintsLine matches the constraints line of a provider block.
	lockConstraintsLine = regexp.MustCompile(`(?m)^(\s*constraints\s*=\s*")[^"]*(")$`)
	// lockFirstH1Line matches any h1: hash line, so a case can replace the first one with a line the script
	// does not read.
	lockFirstH1Line = regexp.MustCompile(`^\s+"h1:`)
	// lockProviderHeaderLine matches a provider block's header line.
	lockProviderHeaderLine = regexp.MustCompile(`^\s*provider\s+"`)
)

// lockCompareCase is one pair of lock files a run compares: what the committed file holds, what the
// regenerated one holds, and the verdict the script must reach.
type lockCompareCase struct {
	name        string   // the subtest's name
	why         string   // the difference between the two files, for the reader
	committed   string   // the lock file git would hold
	regenerated string   // the lock file `tofu providers lock` regenerated
	wantFail    bool     // whether the script must refuse the pair
	wantOut     []string // substrings a [lock-hashes] line must hold, the hash values, the file:line and the provider address; {committed} and {regenerated} stand for the case's two file paths
}

// TestInfraLockCompare pins the script's verdict on the pairs of lock files that differ in one way each.
func TestInfraLockCompare(t *testing.T) {
	t.Parallel()

	scriptPath(t, infraLockCompareScript)

	base := readRepoFile(t, infraLockFile)

	for _, tc := range lockCompareCases(t, base) {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			committed := filepath.Join(dir, "committed.terraform.lock.hcl")
			regenerated := filepath.Join(dir, "regenerated.terraform.lock.hcl")
			writeFileMode(t, committed, []byte(tc.committed), 0o600)
			writeFileMode(t, regenerated, []byte(tc.regenerated), 0o600)

			got := runLockCompare(t, committed, regenerated)

			// A lock file is input: a value that would forge a CI annotation must never come back out, whatever
			// the case's verdict.
			if strings.Contains(got.output(), "##[") {
				t.Errorf("%s echoed %q on %s, want the value named as unprintable: a line holding ##[ forges a GitHub Actions annotation in the CI log: output was:\n%s",
					infraLockCompareScript, "##[", tc.why, got.output())
			}

			if !tc.wantFail {
				if got.exitCode != 0 {
					t.Errorf("%s exited %d on %s, want 0: output was:\n%s", infraLockCompareScript, got.exitCode, tc.why, got.output())
				}

				if strings.Contains(got.output(), infraLockTag) {
					t.Errorf("%s printed %s on %s but must exit 0: output was:\n%s", infraLockCompareScript, infraLockTag, tc.why, got.output())
				}

				return
			}

			if got.exitCode == 0 {
				t.Errorf("%s exited 0 on %s, want a refusal: output was:\n%s", infraLockCompareScript, tc.why, got.output())
			}

			if !strings.Contains(got.output(), infraLockTag) {
				t.Errorf("%s printed no %s line on %s: output was:\n%s", infraLockCompareScript, infraLockTag, tc.why, got.output())
			}

			for _, want := range tc.wantOut {
				want := lockComparePath(want, committed, regenerated)
				if !lockCompareNames(got.output(), infraLockTag, want) {
					t.Errorf("%s printed no %s line holding %q on %s: output was:\n%s", infraLockCompareScript, infraLockTag, want, tc.why, got.output())
				}
			}
		})
	}
}

// lockComparePath resolves the {committed} and {regenerated} placeholders of a case's wantOut, so a case can
// name the file:line it breaks, which lives under t.TempDir() and so differs from run to run.
func lockComparePath(want, committed, regenerated string) string {
	return strings.NewReplacer("{committed}", committed, "{regenerated}", regenerated).Replace(want)
}

// lockCompareNames reports whether some line of text holds both the tag and want: a refusal must name what it
// refuses on the line that reports it, so a provider address or a file:line found anywhere else in the output
// does not stand for it.
func lockCompareNames(text, tag, want string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, tag) && strings.Contains(line, want) {
			return true
		}
	}

	return false
}

// lockCompareCases derives the cases from the committed lock file: each one differs from it in exactly one
// way, so a verdict pins one thing.
func lockCompareCases(t *testing.T, base string) []lockCompareCase {
	t.Helper()

	noH1, h1 := withoutLockHash(t, base, "h1:")
	noZH, zh := withoutLockHash(t, base, "zh:")
	const extra = "h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="

	older := withLockLine(t, base, lockVersionLine, "2.92.0")
	wider := withLockLine(t, base, lockConstraintsLine, ">= 2.93.0")
	unprintableConstraints := withLockLine(t, base, lockConstraintsLine, "##[error]forged")
	unprintableHash := withLockHash(t, base, "h1:##[add-mask]x")
	twoHashesOnOneLine, twoHashesLine := withLockLineReplaced(t, base, lockFirstH1Line, `    "h1:AAA=", "h1:BBB=",`)
	commentedProviderHeader, commentedHeaderLine := withLockLineReplaced(t, base, lockProviderHeaderLine, `provider "`+infraLockProvider+`" { # x`)
	duplicatedHash := withLockHash(t, base, h1)

	return []lockCompareCase{
		{
			name:        "identical-files",
			why:         "a regenerated copy of the committed lock file, byte for byte",
			committed:   base,
			regenerated: base,
		},
		{
			name:        "hashes-in-another-order",
			why:         "a regenerated file whose hash lines are in the reverse order, the same set",
			committed:   base,
			regenerated: reorderLockHashes(base),
		},
		{
			name:        "a-h1-hash-the-committed-file-misses",
			why:         "a committed file without " + h1 + ", one platform's hash the regenerated file holds",
			committed:   noH1,
			regenerated: base,
			wantFail:    true,
			wantOut:     []string{infraLockProvider, h1},
		},
		{
			name:        "a-zh-hash-the-committed-file-misses",
			why:         "a committed file without " + zh + ", a registry checksum the regenerated file holds",
			committed:   noZH,
			regenerated: base,
			wantFail:    true,
			wantOut:     []string{infraLockProvider, zh},
		},
		{
			name:        "an-extra-hash-the-committed-file-holds",
			why:         "a committed file holding " + extra + ", which the regenerated file does not",
			committed:   withLockHash(t, base, extra),
			regenerated: base,
			wantFail:    true,
			wantOut:     []string{infraLockProvider, extra},
		},
		{
			name:        "another-provider-version",
			why:         "a committed file pinning version 2.92.0 where the regenerated one pins the versions.tf pin",
			committed:   older,
			regenerated: base,
			wantFail:    true,
			wantOut:     []string{infraLockProvider},
		},
		{
			name:        "another-constraint",
			why:         `a committed file constraining ">= 2.93.0" where the regenerated one pins it exactly`,
			committed:   wider,
			regenerated: base,
			wantFail:    true,
			wantOut:     []string{infraLockProvider},
		},
		{
			name:        "a-provider-block-the-committed-file-misses",
			why:         "a committed file holding no provider block where the regenerated one holds " + infraLockProvider,
			committed:   withoutProviderBlock(t, base),
			regenerated: base,
			wantFail:    true,
			wantOut:     []string{infraLockProvider},
		},
		{
			name:        "a-provider-block-the-regenerated-file-misses",
			why:         "a committed file holding " + infraLockOtherProvider + " beside the scaleway block, where the regenerated one holds the scaleway block alone",
			committed:   withLockProviderBlock(base, infraLockOtherProvider),
			regenerated: base,
			wantFail:    true,
			wantOut:     []string{infraLockOtherProvider},
		},
		{
			name:        "a-hash-the-committed-file-lists-twice",
			why:         "a committed file listing " + h1 + " twice, where the regenerated one lists it once: the hash set is the same, but a repeated line is no file tofu init writes",
			committed:   duplicatedHash,
			regenerated: base,
			wantFail:    true,
			wantOut:     []string{infraLockProvider, h1},
		},
		{
			name:        "an-unprintable-constraint",
			why:         `a committed file whose constraints are "##[error]forged", a value that would forge a GitHub Actions annotation if the script echoed it`,
			committed:   unprintableConstraints,
			regenerated: base,
			wantFail:    true,
			wantOut:     []string{infraLockProvider, infraLockUnprintableWord},
		},
		{
			name:        "an-unprintable-hash",
			why:         `a committed file holding the hash "h1:##[add-mask]x", which the regenerated file does not`,
			committed:   unprintableHash,
			regenerated: base,
			wantFail:    true,
			wantOut:     []string{infraLockProvider, infraLockUnprintableWord},
		},
		{
			name:        "two-hashes-on-one-line",
			why:         `a committed file and a regenerated one that both write two hashes on one line ("h1:AAA=", "h1:BBB="), a line tofu init never writes`,
			committed:   twoHashesOnOneLine,
			regenerated: twoHashesOnOneLine,
			wantFail:    true,
			// The files differ in nothing, so only a refusal of the unread line itself can fail here.
			wantOut: []string{"{committed}:" + strconv.Itoa(twoHashesLine)},
		},
		{
			name:        "a-provider-header-with-a-trailing-comment",
			why:         "a committed file and a regenerated one that both put a trailing comment on the provider header, a line tofu init never writes",
			committed:   commentedProviderHeader,
			regenerated: commentedProviderHeader,
			wantFail:    true,
			wantOut:     []string{"{committed}:" + strconv.Itoa(commentedHeaderLine)},
		},
	}
}

// withLockLineReplaced returns text with the first line matching pattern replaced by replacement, and the
// number of the line it replaced, so a case can state a line tofu init never writes - one the script must
// refuse as unreadable, at that line - without restating the whole file.
func withLockLineReplaced(t *testing.T, text string, pattern *regexp.Regexp, replacement string) (string, int) {
	t.Helper()

	lines := strings.Split(text, "\n")

	for i, line := range lines {
		if !pattern.MatchString(line) {
			continue
		}

		lines[i] = replacement

		return strings.Join(lines, "\n"), i + 1
	}

	t.Fatalf("the lock file holds no line matching %s: the case names one it can replace", pattern)

	return "", 0
}

// withLockProviderBlock returns text with a provider block for addr appended, as tofu init writes one: a
// version, its constraints and one hash line of its own.
func withLockProviderBlock(text, addr string) string {
	return text + `
provider "` + addr + `" {
  version     = "5.0.0"
  constraints = "5.0.0"
  hashes = [
    "h1:AAAABBBBCCCCDDDDEEEEFFFFGGGGHHHHIIIIJJJJKKK=",
  ]
}
`
}

// TestInfraLockCompareUsage pins the script's refusals of a call it cannot judge: a missing argument, and a
// file that is not there. Each is a usage error, never a comparison that passes on nothing.
func TestInfraLockCompareUsage(t *testing.T) {
	t.Parallel()

	scriptPath(t, infraLockCompareScript)

	dir := t.TempDir()
	existing := filepath.Join(dir, "x.terraform.lock.hcl")
	writeFileMode(t, existing, []byte(readRepoFile(t, infraLockFile)), 0o600)
	missing := filepath.Join(dir, "missing.terraform.lock.hcl")

	cases := []struct {
		name string
		args []string
	}{
		{name: "no-arguments"},
		{name: "one-argument", args: []string{existing}},
		{name: "a-committed-file-that-is-not-there", args: []string{missing, existing}},
		{name: "a-regenerated-file-that-is-not-there", args: []string{existing, missing}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := runLockCompare(t, tc.args...)

			if got.exitCode == 0 {
				t.Errorf("%s exited 0 with the arguments %q, want a usage error: output was:\n%s",
					infraLockCompareScript, tc.args, got.output())
			}

			if !strings.Contains(strings.ToLower(got.output()), infraLockUsageWord) {
				t.Errorf("%s printed no usage message with the arguments %q: output was:\n%s",
					infraLockCompareScript, tc.args, got.output())
			}
		})
	}
}

// lockCompareRun is one run of the script: what it exited with and what it printed.
type lockCompareRun struct {
	exitCode int
	stdout   string
	stderr   string
}

// output is everything the run printed, on either stream: a refusal and a usage error may go to either.
func (r lockCompareRun) output() string {
	return r.stdout + r.stderr
}

// runLockCompare runs the script with args from the repository root, as the CI step does.
func runLockCompare(t *testing.T, args ...string) lockCompareRun {
	t.Helper()

	cmd := exec.Command("bash", append([]string{infraLockCompareScript}, args...)...)
	cmd.Dir = moduleRoot

	var stdout, stderr strings.Builder

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	var exitErr *exec.ExitError

	err := cmd.Run()
	if errors.As(err, &exitErr) {
		err = nil
	}

	if err != nil {
		t.Fatalf("run %s %s: %v", infraLockCompareScript, strings.Join(args, " "), err)
	}

	got := lockCompareRun{stdout: stdout.String(), stderr: stderr.String()}
	if exitErr != nil {
		got.exitCode = exitErr.ExitCode()
	}

	return got
}

// withoutLockHash returns text with the first hash line whose value starts with prefix removed, and that
// value, so a case can state a hash the other file does not hold.
func withoutLockHash(t *testing.T, text, prefix string) (string, string) {
	t.Helper()

	lines := strings.Split(text, "\n")

	for i, line := range lines {
		match := lockHashLine.FindStringSubmatch(line)
		if match == nil || !strings.HasPrefix(match[2], prefix) {
			continue
		}

		return strings.Join(append(slices.Clone(lines[:i]), lines[i+1:]...), "\n"), match[2]
	}

	t.Fatalf("the lock file holds no %s hash: the case names one it can remove", prefix)

	return "", ""
}

// withLockHash returns text with hash added as a hash line after the last hash line it holds.
func withLockHash(t *testing.T, text, hash string) string {
	t.Helper()

	lines := strings.Split(text, "\n")
	last := -1

	for i, line := range lines {
		if lockHashLine.MatchString(line) {
			last = i
		}
	}

	if last < 0 {
		t.Fatalf("the lock file holds no hash line: the case names one it can add a hash beside")

		return text
	}

	added := make([]string, 0, len(lines)+1)
	added = append(added, lines[:last+1]...)
	added = append(added, lockHashLine.FindStringSubmatch(lines[last])[1]+`"`+hash+`",`)
	added = append(added, lines[last+1:]...)

	return strings.Join(added, "\n")
}

// reorderLockHashes returns text with its hash lines in the reverse order: a comparison reads the set of
// hashes, never the order tofu init writes them in.
func reorderLockHashes(text string) string {
	lines := strings.Split(text, "\n")

	var at []int

	var hashes []string

	for i, line := range lines {
		if lockHashLine.MatchString(line) {
			at = append(at, i)
			hashes = append(hashes, line)
		}
	}

	slices.Reverse(hashes)

	for n, i := range at {
		lines[i] = hashes[n]
	}

	return strings.Join(lines, "\n")
}

// withLockLine returns text with the one line the pattern matches (a provider block's version or
// constraints line) holding value instead of what it held.
func withLockLine(t *testing.T, text string, pattern *regexp.Regexp, value string) string {
	t.Helper()

	if n := len(pattern.FindAllString(text, -1)); n != 1 {
		t.Fatalf("the lock file holds %d lines matching %s, want exactly one: the case changes one provider setting", n, pattern)
	}

	return pattern.ReplaceAllStringFunc(text, func(line string) string {
		match := pattern.FindStringSubmatch(line)
		return match[1] + value + match[2]
	})
}

// withoutProviderBlock returns text with its one provider block taken out, header through the line that
// closes it, so a case can state a provider the other file does not hold at all.
func withoutProviderBlock(t *testing.T, text string) string {
	t.Helper()

	lines := strings.Split(text, "\n")
	start, end := -1, -1

	for i, line := range lines {
		if strings.TrimSpace(line) != `provider "`+infraLockProvider+`" {` {
			continue
		}

		if start >= 0 {
			t.Fatalf("the lock file holds the provider block %s twice, want exactly one", infraLockProvider)
		}

		start = i

		for j := i + 1; j < len(lines); j++ {
			if lines[j] == "}" {
				end = j

				break
			}
		}
	}

	if start < 0 || end < 0 {
		t.Fatalf("the lock file holds no closed provider block for %s: the case takes one away", infraLockProvider)
	}

	return strings.Join(append(slices.Clone(lines[:start]), lines[end+1:]...), "\n")
}
