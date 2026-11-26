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
// excluded: git accepts spaces and tabs between the key and its colon.
const coAuthorTrailerKey = "co-authored-by"

// coAuthorAdvice ends every failure of the repository check.
const coAuthorAdvice = "Name the co-author without an address (Co-Authored-By: pi (DeepSeek V4.1 Flash)), " +
	"or with one " + coAuthorAllowListPath + " lists; see the Commits section of .claude/skills/go-tdd/SKILL.md."

// commitMessage is one commit the check reads: its full SHA, its author's and
// committer's email addresses, and its whole message, subject and body. An
// empty address passes: git accepts an empty user.email, and an empty address
// credits no GitHub account; a test that checks only messages leaves both "".
type commitMessage struct {
	SHA       string
	Author    string
	Committer string
	Message   string
}

// coAuthorFinding is one co-author trailer or commit identity the check
// refuses: the commit that holds it, the whole trailer line, trimmed, or
// "author <addr>" / "committer <addr>", and why it is refused.
type coAuthorFinding struct {
	SHA     string
	Trailer string
	Reason  string
}

// coAuthorFindings returns, in commit order, each commit's author and
// committer address that allowed does not hold, then its co-author trailers
// that carry such an address, or a malformed one, in line order. A squash merge
// credits every distinct author of a pull request's commits, so the author and
// committer count as trailers do. A trailer is a line whose trimmed form starts
// with Co-Authored-By in any letter case, then optional spaces or tabs, then a
// colon; one with no @ names nobody's account and passes. Any other must end in
// exactly one <address>, with no @ outside it, whose lower-cased form allowed
// (lower-cased addresses) holds.
func coAuthorFindings(commits []commitMessage, allowed []string) []coAuthorFinding {
	allow := make(map[string]bool, len(allowed))
	for _, addr := range allowed {
		allow[strings.ToLower(addr)] = true
	}

	var findings []coAuthorFinding
	for _, c := range commits {
		for _, id := range []struct{ role, addr string }{{"author", c.Author}, {"committer", c.Committer}} {
			// An empty address (git accepts user.email=) credits no GitHub
			// account, so it passes like an allowed one.
			if id.addr == "" || allow[strings.ToLower(id.addr)] {
				continue
			}
			findings = append(findings, coAuthorFinding{
				SHA:     c.SHA,
				Trailer: id.role + " " + id.addr,
				Reason: fmt.Sprintf("the commit's %s, %s, is not an address of the project (%s): "+
					"a squash merge credits that address's account", id.role, id.addr, coAuthorAllowListPath),
			})
		}
		for line := range strings.SplitSeq(c.Message, "\n") {
			trailer := strings.TrimSpace(line)
			value, ok := coAuthorTrailerValue(trailer)
			if !ok {
				continue
			}
			if reason := coAuthorProblem(value, allow); reason != "" {
				findings = append(findings, coAuthorFinding{SHA: c.SHA, Trailer: trailer, Reason: reason})
			}
		}
	}
	return findings
}

// coAuthorTrailerValue returns the text after the colon of a trimmed line that
// is a co-author trailer, the key in any letter case followed by optional
// spaces or tabs and a colon, and false for any other line, a longer key
// included.
func coAuthorTrailerValue(trailer string) (string, bool) {
	if len(trailer) < len(coAuthorTrailerKey) || !strings.EqualFold(trailer[:len(coAuthorTrailerKey)], coAuthorTrailerKey) {
		return "", false
	}
	rest := strings.TrimLeft(trailer[len(coAuthorTrailerKey):], " \t")
	value, ok := strings.CutPrefix(rest, ":")
	return value, ok
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
	if err := coAuthorCommitCount(os.Getenv, len(commits)); err != nil {
		t.Fatalf("%s: %v", spec, err)
	}

	for _, f := range coAuthorFindings(commits, allowed) {
		t.Errorf("commit %s: %q: %s", f.SHA, f.Trailer, f.Reason)
	}
	if t.Failed() {
		t.Log(coAuthorAdvice)
	}
}

// coAuthorCommitCount returns an error when SHIFT2EU_COAUTHOR_CHECK is
// exactly require and the range read n == 0 commits: a pull request always has
// one, so a required check that read none must fail rather than pass.
func coAuthorCommitCount(getenv func(string) string, n int) error {
	if n == 0 && getenv(coAuthorCheckEnv) == "require" {
		return fmt.Errorf("%s=require, but the range holds no commit: the check read nothing", coAuthorCheckEnv)
	}
	return nil
}

// branchCommits returns the commits of the range spec, newest first, as git
// log writes them: each commit's full SHA, author and committer address and
// raw message, NUL-separated, with git's -z NUL after each commit.
func branchCommits(t *testing.T, root, spec string) []commitMessage {
	t.Helper()

	// --no-show-signature and --no-notes keep a developer's log.showSignature
	// or notes from putting extra text into the -z output.
	out, err := gitOutput(t, root, "log", "-z", "--no-color", "--no-show-signature", "--no-notes", "--format=%H%x00%ae%x00%ce%x00%B", "--end-of-options", spec)
	if err != nil {
		t.Fatalf("git log %s: %v", spec, err)
	}

	commits, err := parseBranchCommits(out)
	if err != nil {
		t.Fatalf("git log %s: %v", spec, err)
	}
	return commits
}

// parseBranchCommits splits git log -z --format=%H%x00%ae%x00%ce%x00%B output
// into commits. Every record is four NUL-separated fields, and -z's NUL closes
// every record but the last, which is accepted with or without one closing
// NUL (one trailing NUL is dropped before the split); a SHA that is not 40 or 64 lower-case hex digits,
// or a field count that is no multiple of four, is an error, never a pass. A
// message cannot hold a NUL (git refuses one), so the split is exact.
func parseBranchCommits(out []byte) ([]commitMessage, error) {
	if len(out) == 0 {
		return nil, nil
	}
	fields := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	if len(fields)%4 != 0 {
		return nil, fmt.Errorf("%d NUL-separated fields, want a SHA, an author, a committer and a message per commit",
			len(fields))
	}

	commits := make([]commitMessage, 0, len(fields)/4)
	for i := 0; i < len(fields); i += 4 {
		sha := fields[i]
		if !isCommitSHA(sha) {
			return nil, fmt.Errorf("record %d starts with %q, want a full commit SHA", i/4+1, sha)
		}
		commits = append(commits, commitMessage{SHA: sha, Author: fields[i+1], Committer: fields[i+2], Message: fields[i+3]})
	}
	return commits, nil
}

// isCommitSHA reports whether s is a full SHA-1 or SHA-256 commit name in
// lower-case hex.
func isCommitSHA(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	return !strings.ContainsFunc(s, func(c rune) bool { return (c < '0' || c > '9') && (c < 'a' || c > 'f') })
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
