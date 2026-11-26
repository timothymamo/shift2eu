package lint_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// coAuthorAllowListPath is the one allow list of co-author addresses, relative
// to the module root (issue #663): a Co-Authored-By trailer that carries an
// address must carry one this file names.
const coAuthorAllowListPath = ".github/co-author-addresses.txt"

// The environment the repository check reads: SHIFT2EU_COAUTHOR_RANGE names the
// revision range, base..head, CI's pull request run passes, and
// SHIFT2EU_COAUTHOR_CHECK=require turns a range the run cannot find into an
// error rather than a skip.
const (
	coAuthorRangeEnv = "SHIFT2EU_COAUTHOR_RANGE"
	coAuthorCheckEnv = "SHIFT2EU_COAUTHOR_CHECK"
)

// coAuthorTrailerKey is the lower-cased key of a co-author trailer, colon
// included.
const coAuthorTrailerKey = "co-authored-by:"

// coAuthorAdvice ends every failure of the repository check.
const coAuthorAdvice = "Name the co-author without an address (Co-Authored-By: pi (DeepSeek V4.1 Flash)), " +
	"or with one " + coAuthorAllowListPath + " lists; see the Commits section of .claude/skills/go-tdd/SKILL.md."

// commitMessage is one commit the check reads: its full SHA and its whole
// message, subject and body.
type commitMessage struct {
	SHA     string
	Message string
}

// coAuthorFinding is one co-author trailer the check refuses: the commit that
// holds it, the whole trailer line, trimmed, and why it is refused.
type coAuthorFinding struct {
	SHA     string
	Trailer string
	Reason  string
}

// coAuthorFindings returns the co-author trailers of commits that carry an
// address allowed does not hold, or a malformed one, in commit order, then in
// line order. A trailer is a line whose trimmed form starts with
// Co-Authored-By: in any letter case; one with no @ names nobody's account and
// passes. Any other must end in exactly one <address>, with no @ outside it,
// whose lower-cased form allowed (lower-cased addresses) holds.
func coAuthorFindings(commits []commitMessage, allowed []string) []coAuthorFinding {
	allow := make(map[string]bool, len(allowed))
	for _, addr := range allowed {
		allow[strings.ToLower(addr)] = true
	}

	var findings []coAuthorFinding
	for _, c := range commits {
		for line := range strings.SplitSeq(c.Message, "\n") {
			trailer := strings.TrimSpace(line)
			if !strings.HasPrefix(strings.ToLower(trailer), coAuthorTrailerKey) {
				continue
			}
			if reason := coAuthorProblem(trailer[len(coAuthorTrailerKey):], allow); reason != "" {
				findings = append(findings, coAuthorFinding{SHA: c.SHA, Trailer: trailer, Reason: reason})
			}
		}
	}
	return findings
}

// coAuthorProblem returns why the value of a co-author trailer, the text after
// its key, is refused, or "" when it is not.
func coAuthorProblem(value string, allow map[string]bool) string {
	if !strings.Contains(value, "@") {
		return ""
	}

	value = strings.TrimSpace(value)
	open := strings.LastIndex(value, "<")
	if open < 0 || !strings.HasSuffix(value, ">") {
		return "the address is not written as one <address> at the end of the trailer"
	}

	name, addr := value[:open], value[open+1:len(value)-1]
	switch {
	case strings.ContainsAny(name, "@<>"):
		return "the trailer holds an @ or an angle bracket outside its one <address>"
	case strings.ContainsAny(addr, "<> \t") || strings.Count(addr, "@") != 1:
		return fmt.Sprintf("<%s> is no well-formed address", addr)
	case !allow[strings.ToLower(addr)]:
		return fmt.Sprintf("%s is not an address of the project (%s): GitHub credits that address's account",
			addr, coAuthorAllowListPath)
	}
	return ""
}

// parseCoAuthorAllowList reads the allow list: one address per line, with
// surrounding white space, blank lines and lines starting with # skipped,
// lower-cased, in the file's order. A line that is no single address (white
// space or an angle bracket inside it, not exactly one @) and an address listed
// twice in any letter case are errors.
func parseCoAuthorAllowList(data []byte) ([]string, error) {
	var addrs []string
	seen := make(map[string]int)
	for i, line := range strings.Split(string(data), "\n") {
		num := i + 1
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.ContainsAny(line, " \t\v\f\r<>") || strings.Count(line, "@") != 1 {
			return nil, fmt.Errorf("line %d: %q is not one address (no white space or angle brackets, "+
				"exactly one @)", num, line)
		}
		addr := strings.ToLower(line)
		if first, ok := seen[addr]; ok {
			return nil, fmt.Errorf("line %d: %s is already listed on line %d", num, addr, first)
		}
		seen[addr] = num
		addrs = append(addrs, addr)
	}
	return addrs, nil
}

// coAuthorRange picks the revision range the check reads, and returns exactly
// one of a range, a reason to skip and an error. An explicit
// SHIFT2EU_COAUTHOR_RANGE wins and must be a plain two-dot range, a..b, of two
// non-empty sides with no white space and no leading dash; failing that,
// SHIFT2EU_COAUTHOR_CHECK=require is an error, so CI never passes a check that
// read nothing; failing that, the branch's commits over origin/main when
// refExists finds it, and otherwise a skip.
func coAuthorRange(getenv func(string) string, refExists func(string) bool) (spec, skip string, err error) {
	if r := getenv(coAuthorRangeEnv); r != "" {
		if err := validCoAuthorRange(r); err != nil {
			return "", "", fmt.Errorf("%s=%q: %w", coAuthorRangeEnv, r, err)
		}
		return r, "", nil
	}
	if getenv(coAuthorCheckEnv) == "require" {
		return "", "", fmt.Errorf("%s=require, but %s names no range: the check would read no commit",
			coAuthorCheckEnv, coAuthorRangeEnv)
	}
	if refExists("origin/main") {
		return "origin/main..HEAD", "", nil
	}
	return "", "no origin/main to compare the branch with and no " + coAuthorRangeEnv +
		" set, so no commit of the branch is known (a shallow checkout); CI's co-author trailer job reads the pull request's range", nil
}

// validCoAuthorRange returns an error unless r is a two-dot range of two
// non-empty sides, with no white space, no leading dash and no third dot.
func validCoAuthorRange(r string) error {
	if strings.ContainsFunc(r, func(c rune) bool { return c == ' ' || (c >= '\t' && c <= '\r') }) {
		return errors.New("a range holds no white space")
	}
	if strings.Contains(r, "...") {
		return errors.New("want a two-dot range a..b, not a three-dot one")
	}
	if strings.Count(r, "..") != 1 {
		return errors.New("want exactly one two-dot range a..b")
	}
	base, head, _ := strings.Cut(r, "..")
	if base == "" || head == "" {
		return errors.New("both sides of the range must name a revision")
	}
	if strings.HasPrefix(base, "-") || strings.HasPrefix(head, "-") {
		return errors.New("a revision never starts with a dash")
	}
	return nil
}

// TestBranchCoAuthorTrailers reads the branch's own commits, the range
// coAuthorRange picks, and fails on every co-author trailer coAuthorFindings
// refuses against .github/co-author-addresses.txt (issue #663). It never reads
// main's history before the range, which holds the 25 commits that credit
// pi-mono. CI's co-author trailer job passes the pull request's range and
// requires one; every other run skips without origin/main.
func TestBranchCoAuthorTrailers(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)

	spec, skip, err := coAuthorRange(os.Getenv, func(ref string) bool {
		return gitIn(t, root, "rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}") == nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if skip != "" {
		t.Skip(skip)
	}

	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(coAuthorAllowListPath)))
	if err != nil {
		t.Fatalf("read %s: %v", coAuthorAllowListPath, err)
	}
	allowed, err := parseCoAuthorAllowList(data)
	if err != nil {
		t.Fatalf("parse %s: %v", coAuthorAllowListPath, err)
	}

	commits := branchCommits(t, root, spec)
	t.Logf("read %d commits of %s", len(commits), spec)

	for _, f := range coAuthorFindings(commits, allowed) {
		t.Errorf("commit %s: %q: %s", f.SHA, f.Trailer, f.Reason)
	}
	if t.Failed() {
		t.Log(coAuthorAdvice)
	}
}

// branchCommits returns the commits of the range spec, newest first, as git
// log writes them: each commit's full SHA and raw message, NUL-separated.
func branchCommits(t *testing.T, root, spec string) []commitMessage {
	t.Helper()

	out, err := gitOutput(t, root, "log", "-z", "--no-color", "--format=%H%x00%B", "--end-of-options", spec)
	if err != nil {
		t.Fatalf("git log %s: %v", spec, err)
	}

	fields := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	if len(fields) == 1 && fields[0] == "" {
		return nil
	}
	if len(fields)%2 != 0 {
		t.Fatalf("git log %s: %d NUL-separated fields, want a SHA and a message per commit", spec, len(fields))
	}

	commits := make([]commitMessage, 0, len(fields)/2)
	for i := 0; i < len(fields); i += 2 {
		commits = append(commits, commitMessage{SHA: fields[i], Message: fields[i+1]})
	}
	return commits
}

// gitIn runs git with args in root and returns its error, if any.
func gitIn(t *testing.T, root string, args ...string) error {
	t.Helper()

	_, err := gitOutput(t, root, args...)
	return err
}

// gitOutput runs git with args, an argv and never a shell, in root, in the
// environment childEnv passes, and returns its standard output, or an error
// that holds its standard error.
func gitOutput(t *testing.T, root string, args ...string) ([]byte, error) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = root
	cmd.Env = childEnv()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}
