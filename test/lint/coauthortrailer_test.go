// Pins the co-author trailer check of issue #663: a commit whose Co-Authored-By
// trailer names an address the project does not own must be refused, because
// GitHub credits that address's account and a squash merge copies the trailers
// of a pull request's commits onto main.
//
// The tester's commits used to end with
//
//	Co-Authored-By: pi <noreply@pi.dev>
//
// which GitHub resolves to the third-party account pi-mono, 25 commits on main
// crediting it. The go-tdd skill's Commits section now asks for
//
//	Co-Authored-By: pi (DeepSeek V4.1 Flash)
//
// with no address at all, which credits nobody; a trailer that carries an
// address must carry one the project uses. The allowed addresses are one list,
// .github/co-author-addresses.txt (coAuthorAllowListPath), documented beside
// that section, so a new agent, App or maintainer address is one line in one
// file.
//
// A commit's author and committer count too: a squash merge credits every
// distinct author of the pull request's commits, so a commit written by an
// address the project does not own is refused with no trailer at all.
//
// The check reads a branch's own commits, the range coAuthorRange picks: the
// pull request's commits in CI, where a range the run cannot read, or one that
// reads no commit, is an error rather than a skip, so a pull request is never
// passed by a check that read nothing. TestCoAuthorRange and
// TestCoAuthorCommitCount pin that choice without git; the helpers
// (coAuthorFindings, parseCoAuthorAllowList, coAuthorRange, coAuthorCommitCount
// and coAuthorAllowListPath) live in coauthortrailer_rule_test.go, and the case
// that runs git over the branch beside it.
package lint_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// TestCoAuthorFindings pins which co-author trailers coAuthorFindings refuses:
// every trailer that carries an address the allow list does not hold, named in
// any letter case, and every trailer whose address is malformed, so a bad
// address can never slip through by not looking like one. A trailer with no
// address at all, the form the go-tdd skill asks for, is fine.
//
// A commit's author and committer are checked against the same list, because a
// squash merge credits every distinct author of the pull request's commits: a
// commit written by an address the project does not own is refused even with no
// trailer at all, and its finding names the address as "author <addr>" or
// "committer <addr>".
//
// The findings come in commit order: the author, then the committer, then the
// message's trailers in line order. A trailer's finding names the whole
// offending line as it is written, so a report points at the line to edit. The
// key must start the trimmed line, in any letter case and with white space
// before the colon, as git accepts it: prose that mentions Co-Authored-By mid
// sentence is no trailer and no finding.
func TestCoAuthorFindings(t *testing.T) {
	t.Parallel()

	// The commits the cases below read, as git writes a full SHA.
	const (
		firstCommit  = "1111111111111111111111111111111111111111"
		secondCommit = "2222222222222222222222222222222222222222"
	)

	// allowed are the addresses the cases are read against: the Anthropic address
	// a Claude Code commit carries and the agents App's noreply address.
	allowed := []string{
		"noreply@anthropic.com",
		"357791782+shift2eu-agents[bot]@users.noreply.github.com",
	}

	tests := []struct {
		name    string
		commits []commitMessage
		want    []coAuthorFinding
	}{
		{
			name: "no trailer",
			commits: []commitMessage{{
				SHA:     firstCommit,
				Message: "feat(lint): a subject\n\nA body that names pi@earendil-works.com in prose.\n",
			}},
			want: nil,
		},
		{
			name:    "no commits",
			commits: nil,
			want:    nil,
		},
		{
			name: "a trailer with no address",
			commits: []commitMessage{{
				SHA:     firstCommit,
				Message: "feat(lint): a subject\n\nCo-Authored-By: pi (DeepSeek V4.1 Flash)\n",
			}},
			want: nil,
		},
		{
			name: "a trailer with an allowed address",
			commits: []commitMessage{{
				SHA:     firstCommit,
				Message: "feat(lint): a subject\n\nCo-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>\n",
			}},
			want: nil,
		},
		{
			name: "a trailer with an allowed App address",
			commits: []commitMessage{{
				SHA: firstCommit,
				Message: "feat(lint): a subject\n\n" +
					"Co-Authored-By: shift2eu-agents[bot] <357791782+shift2eu-agents[bot]@users.noreply.github.com>\n",
			}},
			want: nil,
		},
		{
			name: "an allowed address in another letter case",
			commits: []commitMessage{{
				SHA:     firstCommit,
				Message: "feat(lint): a subject\n\nCo-Authored-By: Claude Opus 5.5 <NoReply@Anthropic.com>\n",
			}},
			want: nil,
		},
		{
			name: "noreply@pi.dev",
			commits: []commitMessage{{
				SHA:     firstCommit,
				Message: "feat(lint): a subject\n\nCo-Authored-By: pi <noreply@pi.dev>\n",
			}},
			want: []coAuthorFinding{{
				SHA:     firstCommit,
				Trailer: "Co-Authored-By: pi <noreply@pi.dev>",
			}},
		},
		{
			name: "noreply@deepseek.com",
			commits: []commitMessage{{
				SHA:     firstCommit,
				Message: "feat(lint): a subject\n\nCo-Authored-By: pi <noreply@deepseek.com>\n",
			}},
			want: []coAuthorFinding{{
				SHA:     firstCommit,
				Trailer: "Co-Authored-By: pi <noreply@deepseek.com>",
			}},
		},
		{
			name: "noreply@earendil-works.com",
			commits: []commitMessage{{
				SHA:     firstCommit,
				Message: "feat(lint): a subject\n\nCo-Authored-By: pi <noreply@earendil-works.com>\n",
			}},
			want: []coAuthorFinding{{
				SHA:     firstCommit,
				Trailer: "Co-Authored-By: pi <noreply@earendil-works.com>",
			}},
		},
		{
			name: "pi@earendil-works.com",
			commits: []commitMessage{{
				SHA:     firstCommit,
				Message: "feat(lint): a subject\n\nCo-Authored-By: pi <pi@earendil-works.com>\n",
			}},
			want: []coAuthorFinding{{
				SHA:     firstCommit,
				Trailer: "Co-Authored-By: pi <pi@earendil-works.com>",
			}},
		},
		{
			name: "the key in any letter case",
			commits: []commitMessage{{
				SHA: firstCommit,
				Message: "feat(lint): a subject\n\n" +
					"co-authored-by: pi <noreply@pi.dev>\n" +
					"CO-AUTHORED-BY: pi <noreply@deepseek.com>\n" +
					"Co-authored-by: pi <noreply@earendil-works.com>\n",
			}},
			want: []coAuthorFinding{
				{SHA: firstCommit, Trailer: "co-authored-by: pi <noreply@pi.dev>"},
				{SHA: firstCommit, Trailer: "CO-AUTHORED-BY: pi <noreply@deepseek.com>"},
				{SHA: firstCommit, Trailer: "Co-authored-by: pi <noreply@earendil-works.com>"},
			},
		},
		{
			name: "an address the project does not use",
			commits: []commitMessage{{
				SHA:     firstCommit,
				Message: "feat(lint): a subject\n\nCo-Authored-By: Someone <someone@example.com>\n",
			}},
			want: []coAuthorFinding{{
				SHA:     firstCommit,
				Trailer: "Co-Authored-By: Someone <someone@example.com>",
			}},
		},
		{
			name: "an address outside the angle brackets",
			commits: []commitMessage{{
				SHA:     firstCommit,
				Message: "feat(lint): a subject\n\nCo-Authored-By: pi noreply@pi.dev\n",
			}},
			want: []coAuthorFinding{{
				SHA:     firstCommit,
				Trailer: "Co-Authored-By: pi noreply@pi.dev",
			}},
		},
		{
			name: "an unclosed angle bracket",
			commits: []commitMessage{{
				SHA:     firstCommit,
				Message: "feat(lint): a subject\n\nCo-Authored-By: pi <noreply@pi.dev\n",
			}},
			want: []coAuthorFinding{{
				SHA:     firstCommit,
				Trailer: "Co-Authored-By: pi <noreply@pi.dev",
			}},
		},
		{
			name: "leading whitespace before the key",
			commits: []commitMessage{{
				SHA:     firstCommit,
				Message: "feat(lint): a subject\n\n  Co-Authored-By: x <noreply@pi.dev>\n",
			}},
			want: []coAuthorFinding{{
				SHA:     firstCommit,
				Trailer: "Co-Authored-By: x <noreply@pi.dev>",
			}},
		},
		{
			name: "white space before the colon",
			commits: []commitMessage{{
				SHA: firstCommit,
				Message: "feat(lint): a subject\n\n" +
					"Co-Authored-By : pi <noreply@pi.dev>\n" +
					"co-authored-by\t: pi <noreply@pi.dev>\n",
			}},
			want: []coAuthorFinding{
				{SHA: firstCommit, Trailer: "Co-Authored-By : pi <noreply@pi.dev>"},
				{SHA: firstCommit, Trailer: "co-authored-by\t: pi <noreply@pi.dev>"},
			},
		},
		{
			name: "white space before the colon of a trailer with no address",
			commits: []commitMessage{{
				SHA:     firstCommit,
				Message: "feat(lint): a subject\n\nCo-Authored-By : pi (DeepSeek V4.1 Flash)\n",
			}},
			want: nil,
		},
		{
			name: "a key that only starts with the co-author one",
			commits: []commitMessage{{
				SHA:     firstCommit,
				Message: "feat(lint): a subject\n\nCo-Authored-Byline: x <noreply@pi.dev>\n",
			}},
			want: nil,
		},
		{
			name: "an author address the project does not own",
			commits: []commitMessage{{
				SHA:     firstCommit,
				Author:  "noreply@pi.dev",
				Message: "feat(lint): a subject\n\nA body with no trailer.\n",
			}},
			want: []coAuthorFinding{{
				SHA:     firstCommit,
				Trailer: "author noreply@pi.dev",
			}},
		},
		{
			name: "a committer address the project does not own",
			commits: []commitMessage{{
				SHA:       firstCommit,
				Committer: "noreply@deepseek.com",
				Message:   "feat(lint): a subject\n\nA body with no trailer.\n",
			}},
			want: []coAuthorFinding{{
				SHA:     firstCommit,
				Trailer: "committer noreply@deepseek.com",
			}},
		},
		{
			name: "an App as author and committer, with a trailer of no address",
			commits: []commitMessage{{
				SHA:       firstCommit,
				Author:    "357791782+shift2eu-agents[bot]@users.noreply.github.com",
				Committer: "357791782+shift2eu-agents[bot]@users.noreply.github.com",
				Message:   "feat(lint): a subject\n\nCo-Authored-By: pi (DeepSeek V4.1 Flash)\n",
			}},
			want: nil,
		},
		{
			name: "an author address in another letter case",
			commits: []commitMessage{{
				SHA:     firstCommit,
				Author:  "NoReply@Anthropic.com",
				Message: "feat(lint): a subject\n\nCo-Authored-By: pi (DeepSeek V4.1 Flash)\n",
			}},
			want: nil,
		},
		{
			name: "an author, a committer and a trailer the project does not own",
			commits: []commitMessage{{
				SHA:       firstCommit,
				Author:    "noreply@pi.dev",
				Committer: "noreply@deepseek.com",
				Message:   "feat(lint): a subject\n\nCo-Authored-By: pi <noreply@earendil-works.com>\n",
			}},
			want: []coAuthorFinding{
				{SHA: firstCommit, Trailer: "author noreply@pi.dev"},
				{SHA: firstCommit, Trailer: "committer noreply@deepseek.com"},
				{SHA: firstCommit, Trailer: "Co-Authored-By: pi <noreply@earendil-works.com>"},
			},
		},
		{
			name: "the words mid-sentence are no trailer",
			commits: []commitMessage{{
				SHA: firstCommit,
				Message: "feat(lint): a subject\n\n" +
					"see the Co-Authored-By: rule in the go-tdd skill\n" +
					"See Co-Authored-By: pi <noreply@pi.dev> for the rule\n",
			}},
			want: nil,
		},
		{
			name: "one good trailer and one bad in a commit",
			commits: []commitMessage{{
				SHA: firstCommit,
				Message: "feat(lint): a subject\n\n" +
					"Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>\n" +
					"Co-Authored-By: pi <noreply@pi.dev>\n",
			}},
			want: []coAuthorFinding{{
				SHA:     firstCommit,
				Trailer: "Co-Authored-By: pi <noreply@pi.dev>",
			}},
		},
		{
			name: "several commits, several trailers",
			commits: []commitMessage{
				{
					SHA: firstCommit,
					Message: "feat(lint): a subject\n\nA body.\n\n" +
						"Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>\n" +
						"Co-Authored-By: pi <noreply@pi.dev>\n" +
						"Co-Authored-By: pi (DeepSeek V4.1 Flash)\n" +
						"Co-Authored-By: pi <noreply@deepseek.com>\n",
				},
				{
					SHA:     secondCommit,
					Message: "docs(lint): another subject\n\nCo-Authored-By: pi <pi@earendil-works.com>\n",
				},
			},
			want: []coAuthorFinding{
				{SHA: firstCommit, Trailer: "Co-Authored-By: pi <noreply@pi.dev>"},
				{SHA: firstCommit, Trailer: "Co-Authored-By: pi <noreply@deepseek.com>"},
				{SHA: secondCommit, Trailer: "Co-Authored-By: pi <pi@earendil-works.com>"},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := coAuthorFindings(tc.commits, allowed)

			// The reason a finding gives is human text, pinned by no case here: the
			// check is the address, not the wording.
			diff := cmp.Diff(tc.want, got,
				cmpopts.EquateEmpty(),
				cmpopts.IgnoreFields(coAuthorFinding{}, "Reason"))
			if diff != "" {
				t.Errorf("coAuthorFindings(%v) mismatch (-want +got):\n%s", tc.commits, diff)
			}
		})
	}
}

// TestParseBranchCommits pins how the check splits git log's output back into
// commits. It runs
//
//	git log -z --format=%H%x00%ae%x00%ce%x00%B
//
// which writes, per commit, the full SHA, the author's and committer's
// addresses and the whole message, each closed by a NUL, and with -z one more
// NUL after the record, so a record is SHA\0author\0committer\0message\0. The
// last record closes the output and arrives with its NUL or, read another way,
// without it. A message holds no NUL (git refuses one), so the split is exact
// and an empty message is the fourth field and nothing more, in the middle of
// the output as at its end; a message that holds a blank line and a trailer
// survives byte for byte.
//
// Output the check cannot read exactly is an error, never a pass: a field count
// that is no multiple of four, and a record whose first field is no full commit
// name. git writes a commit name as 40 lower-case hex digits, or 64 in a
// SHA-256 repository, so a short, upper-case or non-hex name is refused rather
// than turned into a commit the report could not name.
func TestParseBranchCommits(t *testing.T) {
	t.Parallel()

	const (
		first  = "1111111111111111111111111111111111111111"
		second = "2222222222222222222222222222222222222222"

		sha256Name = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

		author    = "author@example.com"
		committer = "committer@example.com"
	)

	// record returns one commit as git log -z writes it, its closing NUL included.
	record := func(sha, a, c, msg string) string {
		return sha + "\x00" + a + "\x00" + c + "\x00" + msg + "\x00"
	}

	// trailerMessage is a message with a subject, a body whose paragraph is
	// followed by a blank line and a co-author trailer: the shape the check
	// reads, and one no field split may touch.
	const trailerMessage = "feat(lint): a subject\n\nA body.\n\nCo-Authored-By: pi <pi@example.com>\n"

	tests := []struct {
		name    string
		out     string
		want    []commitMessage
		wantErr bool
	}{
		{
			name: "no output",
			out:  "",
			want: nil,
		},
		{
			name: "one record",
			out:  record(first, author, committer, "feat(lint): a subject\n"),
			want: []commitMessage{{SHA: first, Author: author, Committer: committer, Message: "feat(lint): a subject\n"}},
		},
		{
			name: "the last record without its closing NUL",
			out:  first + "\x00" + author + "\x00" + committer + "\x00feat(lint): a subject\n",
			want: []commitMessage{{SHA: first, Author: author, Committer: committer, Message: "feat(lint): a subject\n"}},
		},
		{
			name: "two records, in the log's order",
			out: record(first, author, committer, "feat(lint): a subject\n") +
				record(second, "author2@example.com", "committer2@example.com", "docs(lint): another\n"),
			want: []commitMessage{
				{SHA: first, Author: author, Committer: committer, Message: "feat(lint): a subject\n"},
				{
					SHA:       second,
					Author:    "author2@example.com",
					Committer: "committer2@example.com",
					Message:   "docs(lint): another\n",
				},
			},
		},
		{
			name: "an empty message in the last record",
			out:  first + "\x00" + author + "\x00" + committer + "\x00\x00",
			want: []commitMessage{{SHA: first, Author: author, Committer: committer, Message: ""}},
		},
		{
			// The record's own NUL follows the empty message's NUL, so the bytes
			// between the committer and the second SHA are two NULs, not one.
			name: "an empty message in the middle record",
			out: record(first, author, committer, "") +
				record(second, "author2@example.com", "committer2@example.com", "docs(lint): another\n"),
			want: []commitMessage{
				{SHA: first, Author: author, Committer: committer, Message: ""},
				{
					SHA:       second,
					Author:    "author2@example.com",
					Committer: "committer2@example.com",
					Message:   "docs(lint): another\n",
				},
			},
		},
		{
			name: "a message with a blank line and a trailer",
			out:  record(first, author, committer, trailerMessage),
			want: []commitMessage{{SHA: first, Author: author, Committer: committer, Message: trailerMessage}},
		},
		{
			name:    "three fields",
			out:     first + "\x00" + author + "\x00" + committer + "\x00",
			wantErr: true,
		},
		{
			name:    "five fields",
			out:     record(first, author, committer, "feat(lint): a subject\n") + "extra\x00",
			wantErr: true,
		},
		{
			name:    "seven fields",
			out:     record(first, author, committer, "feat(lint): a subject\n") + "extra\x00more\x00",
			wantErr: true,
		},
		{
			name:    "a short SHA",
			out:     record("abc1234", author, committer, "feat(lint): a subject\n"),
			wantErr: true,
		},
		{
			name:    "an upper-case SHA",
			out:     record(strings.Repeat("A", 40), author, committer, "feat(lint): a subject\n"),
			wantErr: true,
		},
		{
			name:    "a SHA that is not hex",
			out:     record(strings.Repeat("g", 40), author, committer, "feat(lint): a subject\n"),
			wantErr: true,
		},
		{
			name: "a 64-hex SHA, as a SHA-256 repository writes it",
			out:  record(sha256Name, author, committer, "feat(lint): a subject\n"),
			want: []commitMessage{{SHA: sha256Name, Author: author, Committer: committer, Message: "feat(lint): a subject\n"}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseBranchCommits([]byte(tc.out))

			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseBranchCommits(%q) = %v, want an error", tc.out, got)
				}
				if got != nil {
					t.Errorf("parseBranchCommits(%q) = %v with the error %v; want no commit", tc.out, got, err)
				}

				return
			}

			if err != nil {
				t.Fatalf("parseBranchCommits(%q): %v", tc.out, err)
			}
			if diff := cmp.Diff(tc.want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("parseBranchCommits(%q) mismatch (-want +got):\n%s", tc.out, diff)
			}
		})
	}
}

// TestParseCoAuthorAllowList pins how .github/co-author-addresses.txt is read:
// one address per line, lower-cased, with blank lines and comment lines skipped,
// so the file may say why each address is there. A line that is not an address
// is an error rather than a line the check silently ignores: a misspelt
// address, a whole trailer line pasted into the file, or a line that holds two
// addresses would otherwise leave the check allowing nothing the file names.
// A case-insensitive duplicate is an error too, because the file is one list.
func TestParseCoAuthorAllowList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		data    string
		want    []string
		wantErr bool
	}{
		{
			name: "empty file",
			data: "",
			want: nil,
		},
		{
			name: "one address",
			data: "noreply@anthropic.com\n",
			want: []string{"noreply@anthropic.com"},
		},
		{
			name: "no trailing newline",
			data: "noreply@anthropic.com",
			want: []string{"noreply@anthropic.com"},
		},
		{
			name: "comments, blank lines and surrounding spaces",
			data: "# the agents' App\n\n   \n\t\n  # the release App  \n" +
				"  357791782+shift2eu-agents[bot]@users.noreply.github.com  \n",
			want: []string{"357791782+shift2eu-agents[bot]@users.noreply.github.com"},
		},
		{
			name: "mixed case is lower-cased",
			data: "NoReply@Anthropic.COM\n",
			want: []string{"noreply@anthropic.com"},
		},
		{
			name: "two addresses keep the file's order",
			data: "noreply@anthropic.com\n357791782+shift2eu-agents[bot]@users.noreply.github.com\n",
			want: []string{
				"noreply@anthropic.com",
				"357791782+shift2eu-agents[bot]@users.noreply.github.com",
			},
		},
		{
			name:    "a space inside the address",
			data:    "noreply @anthropic.com\n",
			wantErr: true,
		},
		{
			name:    "a space after the at sign",
			data:    "noreply@ anthropic.com\n",
			wantErr: true,
		},
		{
			name:    "an opening angle bracket",
			data:    "<noreply@anthropic.com>\n",
			wantErr: true,
		},
		{
			name:    "a closing angle bracket",
			data:    "noreply@anthropic.com>\n",
			wantErr: true,
		},
		{
			name:    "no at sign",
			data:    "noreply.anthropic.com\n",
			wantErr: true,
		},
		{
			name:    "two at signs",
			data:    "a@b@example.com\n",
			wantErr: true,
		},
		{
			name:    "a duplicate in another letter case",
			data:    "noreply@anthropic.com\nNoReply@Anthropic.com\n",
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseCoAuthorAllowList([]byte(tc.data))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseCoAuthorAllowList(%q) = %v, want an error", tc.data, got)
				}

				return
			}
			if err != nil {
				t.Fatalf("parseCoAuthorAllowList(%q): %v", tc.data, err)
			}
			if diff := cmp.Diff(tc.want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("parseCoAuthorAllowList(%q) mismatch (-want +got):\n%s", tc.data, diff)
			}
		})
	}
}

// TestCoAuthorRange pins which revision range the check reads, from the
// environment alone: the range an explicit SHIFT2EU_COAUTHOR_RANGE names, or,
// failing that, the branch's own commits over origin/main. A range it cannot
// read is an error rather than a skip whenever SHIFT2EU_COAUTHOR_CHECK asks for
// one, which CI does: a check that could not read the pull request's commits
// must never report a pass. That demand holds even when origin/main is there, so
// no run falls back to origin/main..HEAD in silence. Without that demand, and
// with no origin/main to compare against, the check skips and says why.
func TestCoAuthorRange(t *testing.T) {
	t.Parallel()

	// Two commits a full length SHA names, as CI passes them.
	const (
		rangeKey = coAuthorRangeEnv
		checkKey = coAuthorCheckEnv

		base = "1111111111111111111111111111111111111111"
		head = "2222222222222222222222222222222222222222"

		originMain = "origin/main"
	)

	tests := []struct {
		name     string
		env      map[string]string
		refs     []string
		wantSpec string
		wantSkip bool
		wantErr  bool
	}{
		{
			name:     "a range of two commits",
			env:      map[string]string{rangeKey: base + ".." + head},
			wantSpec: base + ".." + head,
		},
		{
			name:     "a range of refs",
			env:      map[string]string{rangeKey: originMain + "..HEAD"},
			wantSpec: originMain + "..HEAD",
		},
		{
			name:    "a range that starts with a dash",
			env:     map[string]string{rangeKey: "-n5..HEAD"},
			wantErr: true,
		},
		{
			name:    "a range with no dots",
			env:     map[string]string{rangeKey: originMain},
			wantErr: true,
		},
		{
			name:    "a range with three dots",
			env:     map[string]string{rangeKey: originMain + "...HEAD"},
			wantErr: true,
		},
		{
			name:    "a range with a space",
			env:     map[string]string{rangeKey: originMain + ".. HEAD"},
			wantErr: true,
		},
		{
			name:    "a range with no left side",
			env:     map[string]string{rangeKey: "..HEAD"},
			wantErr: true,
		},
		{
			name:    "a range with no right side",
			env:     map[string]string{rangeKey: "abc.."},
			wantErr: true,
		},
		{
			name:    "one required in CI with no origin/main",
			env:     map[string]string{checkKey: "require"},
			wantErr: true,
		},
		{
			name:    "one required in CI with origin/main but no range",
			env:     map[string]string{checkKey: "require"},
			refs:    []string{originMain},
			wantErr: true,
		},
		{
			name:     "origin/main in the branch's history",
			refs:     []string{originMain},
			wantSpec: originMain + "..HEAD",
		},
		{
			name:     "no origin/main, so the check skips",
			wantSkip: true,
		},
		{
			name:     "a range wins over one required",
			env:      map[string]string{rangeKey: originMain + "..HEAD", checkKey: "require"},
			refs:     []string{originMain},
			wantSpec: originMain + "..HEAD",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotSpec, gotSkip, err := coAuthorRange(
				func(key string) string { return tc.env[key] },
				func(ref string) bool { return slices.Contains(tc.refs, ref) },
			)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("coAuthorRange() = %q, skip %q, want an error", gotSpec, gotSkip)
				}
				if gotSpec != "" || gotSkip != "" {
					t.Errorf("coAuthorRange() = %q, skip %q with an error %v; want exactly one of a "+
						"range, a skip reason and an error", gotSpec, gotSkip, err)
				}

				return
			}

			if err != nil {
				t.Fatalf("coAuthorRange(): %v", err)
			}
			if diff := cmp.Diff(tc.wantSpec, gotSpec); diff != "" {
				t.Errorf("coAuthorRange() spec mismatch (-want +got):\n%s", diff)
			}
			if (gotSkip != "") != tc.wantSkip {
				t.Errorf("coAuthorRange() skip = %q, want a non-empty skip reason: %t", gotSkip, tc.wantSkip)
			}
		})
	}
}

// TestCoAuthorCommitCount pins the second half of that demand: a range that the
// run reads and that yields no commit is an error too, because a pull request
// always has one, so a green job never means the check read nothing. Only the
// exact word require demands it, so a typo fails no honest run and passes no
// required one.
func TestCoAuthorCommitCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		env     map[string]string
		n       int
		wantErr bool
	}{
		{
			name:    "one required and no commit",
			env:     map[string]string{coAuthorCheckEnv: "require"},
			wantErr: true,
		},
		{
			name: "one required and a commit",
			env:  map[string]string{coAuthorCheckEnv: "require"},
			n:    1,
		},
		{
			name: "not required and no commit",
		},
		{
			name: "REQUIRE in capitals is not the word",
			env:  map[string]string{coAuthorCheckEnv: "REQUIRE"},
		},
		{
			name: "yes is not the word",
			env:  map[string]string{coAuthorCheckEnv: "yes"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := coAuthorCommitCount(func(key string) string { return tc.env[key] }, tc.n)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("coAuthorCommitCount(%d) = nil, want an error", tc.n)
				}

				return
			}
			if err != nil {
				t.Fatalf("coAuthorCommitCount(%d): %v", tc.n, err)
			}
		})
	}
}

// TestCoAuthorAllowListFile reads the one allow list the check reads,
// .github/co-author-addresses.txt, and pins what it must say: the addresses the
// project's own agents and Apps commit with, and none of the addresses that
// credit a third-party account (issue #663). It is the file the go-tdd skill's
// Commits section documents, so a trailer the skill asks for is a line here.
func TestCoAuthorAllowListFile(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)

	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(coAuthorAllowListPath)))
	if err != nil {
		t.Fatalf("read %s: %v", coAuthorAllowListPath, err)
	}

	got, err := parseCoAuthorAllowList(data)
	if err != nil {
		t.Fatalf("parse %s: %v", coAuthorAllowListPath, err)
	}

	// The addresses the project uses: the agents' Claude Code commits, the
	// agents' App, the release App, the audit App, Renovate's commits, GitHub
	// itself and the maintainer's GitHub noreply address.
	//
	// noreply@github.com is there because GitHub is the committer of every
	// commit made through its API or web interface: release-please's and
	// Renovate's commits on their pull request branches, and every "Update
	// branch". Without it every such pull request fails the check.
	for _, addr := range []string{
		"noreply@anthropic.com",
		"357791782+shift2eu-agents[bot]@users.noreply.github.com",
		"355849575+shift2eu-release[bot]@users.noreply.github.com",
		"359886496+shift2eu-audit[bot]@users.noreply.github.com",
		"29139614+renovate[bot]@users.noreply.github.com",
		"noreply@github.com",
		"65859835+timothymamo@users.noreply.github.com",
	} {
		if !slices.Contains(got, addr) {
			t.Errorf("%s does not list %s, an address the project's agents and Apps commit with: %v",
				coAuthorAllowListPath, addr, got)
		}
	}

	// The addresses the project does not own: GitHub credits each one's account
	// with a commit that carries it (.github/co-author-addresses.txt).
	for _, addr := range []string{
		"noreply@pi.dev",
		"noreply@deepseek.com",
		"noreply@earendil-works.com",
		"pi@earendil-works.com",
	} {
		if slices.Contains(got, addr) {
			t.Errorf("%s lists %s, an address the project does not own: %v",
				coAuthorAllowListPath, addr, got)
		}
	}
}
