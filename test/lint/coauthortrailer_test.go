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
// The check reads a branch's own commits, the range coAuthorRange picks: the
// pull request's commits in CI, where a range the run cannot read is an error
// rather than a skip, so a pull request is never passed by a check that read
// nothing. TestCoAuthorRange pins that choice without git; the helpers
// (coAuthorFindings, parseCoAuthorAllowList, coAuthorRange and
// coAuthorAllowListPath) live in coauthortrailer_rule_test.go, and the case
// that runs git over the branch beside it.
package lint_test

import (
	"os"
	"path/filepath"
	"slices"
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
// The findings come in commit order, then in line order, and name the whole
// offending line as it is written, so a report points at the line to edit. The
// key must start the trimmed line: prose that mentions Co-Authored-By mid
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
// must never report a pass. Without that demand, and with no origin/main to
// compare against, the check skips and says why.
func TestCoAuthorRange(t *testing.T) {
	t.Parallel()

	// Two commits a full length SHA names, as CI passes them.
	const (
		rangeKey = "SHIFT2EU_COAUTHOR_RANGE"
		checkKey = "SHIFT2EU_COAUTHOR_CHECK"

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
	// agents' App, the release App's and Renovate's commits.
	for _, addr := range []string{
		"noreply@anthropic.com",
		"357791782+shift2eu-agents[bot]@users.noreply.github.com",
		"355849575+shift2eu-release[bot]@users.noreply.github.com",
		"29139614+renovate[bot]@users.noreply.github.com",
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
