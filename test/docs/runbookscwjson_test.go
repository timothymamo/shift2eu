// Pins that the site runbook and the guides read the JSON of a
// `scw iam api-key get` command through the wrapper current scw releases print
// (issue #532): R0's `ADMIN_PROJECT` and `ME` lines take the admin key's default
// project and owner with jq filters that start from the key itself.
//
// A current scw CLI answers `scw iam api-key get` with the key inside an
// `APIKey` field ({"APIKey": {"user_id": ..., "default_project_id": ...}, ...}),
// older releases print the key unwrapped. A filter of the key's own fields,
// `.default_project_id`, reads nothing from the wrapper and makes jq print
// `null`, which R0's PASS line reads as a failed call: every filter that a
// `scw iam api-key get ... -o json | jq` line in `docs/site-runbook.md` or
// `docs/guide/*.md` runs therefore starts by falling back from the wrapper to
// the key, `(.APIKey // .)`, and works with both shapes.
//
// The check reads the documents' lines as the repository holds them, so it
// needs neither the scw CLI nor jq: it asserts on the filters' text alone. It
// scopes itself to `scw iam api-key get`, the one command whose output carries
// the APIKey wrapper, and accepts every output spelling scw takes (`-o json`,
// `-o=json`, `-ojson`, `--output json`, `--output=json`). A line that pipes
// another scw command's JSON into jq - `scw object bucket get ... -o json |
// jq`, whose output has no wrapper - is not a filter of the key and is not
// matched. So is the line that pipes `-o json` into something other than jq
// (R6's password manager CLI) and prose about scw.
package docs_test

import (
	"io/fs"
	"os"
	"regexp"
	"strings"
	"testing"
)

const (
	// siteRunbookPath is the site runbook the R0 lines live in.
	siteRunbookPath = "docs/site-runbook.md"

	// guidesGlob is every guide, the other pages a `scw iam api-key get` line
	// may land in. They hold none today; a guide that gains one is checked
	// like the runbook's, but only the runbook owes the floor below.
	guidesGlob = "docs/guide/*.md"

	// scwKeyFallback is the jq path every filter over a
	// `scw iam api-key get ... -o json` line starts with: scw's APIKey wrapper
	// when the CLI prints one, else the key itself (issue #532).
	scwKeyFallback = "(.APIKey // .)"

	// adminProjectPrefix and ownerPrefix are R0's two lines that read the
	// admin's own API key, the two commands whose filter this check pins.
	adminProjectPrefix = `ADMIN_PROJECT="$(scw iam api-key get`
	ownerPrefix        = `ME="$(scw iam api-key get`

	// failEmptyPrefix starts R0's catalogue of the values a failed scw call
	// prints: the line the missing key's shape must be named in.
	failEmptyPrefix = "# FAIL an empty value"

	// minScwJSONLines is how many `scw iam api-key get ... -o json | jq` lines
	// the runbook must keep: R0's two, and never fewer, so a deleted line
	// cannot make the filter check pass on nothing.
	minScwJSONLines = 2
)

// scwAPIKeyJSONPipedToJQ matches a line that runs `scw iam api-key get` with
// JSON output, in any spelling scw accepts, and pipes that output into jq: the
// commands whose filters this check reads. The output flag is `(?:-o|--output)`
// followed by json, with or without a space or `=`, so `-o json`, `-o=json`,
// `-ojson`, `--output json` and `--output=json` all match. A line that pipes
// the JSON elsewhere, another scw command, an aws command and prose about scw
// are not such a command.
var scwAPIKeyJSONPipedToJQ = regexp.MustCompile(`scw iam api-key get\b.*(?:-o|--output)[ =]?json\b.*\|\s*jq\b`)

// TestRunbookReadsTheScwAPIKeyJSONThroughItsWrapper pins that every runbook and
// guide line that pipes `scw iam api-key get ... -o json` into jq reads the key
// through `(.APIKey // .)` (issue #532), that the two lines R0 reads the
// admin's key with are there and do so, and that R0's catalogue of a failed
// call names the value a key scw could not find prints.
func TestRunbookReadsTheScwAPIKeyJSONThroughItsWrapper(t *testing.T) {
	t.Parallel()

	fsys := os.DirFS(repoRoot)
	lines := strings.Split(readDoc(t, fsys, siteRunbookPath), "\n")

	t.Run("every scw api-key JSON line piped into jq reads through the wrapper", func(t *testing.T) {
		t.Parallel()

		for _, doc := range scwJSONDocs(t, fsys) {
			docLines := strings.Split(readDoc(t, fsys, doc), "\n")
			var matched []int

			for i, line := range docLines {
				if !scwAPIKeyJSONPipedToJQ.MatchString(line) {
					continue
				}

				matched = append(matched, i+1)

				if !strings.Contains(line, scwKeyFallback) {
					t.Errorf("%s:%d pipes scw's api-key JSON into jq without %s, so the filter reads a field of scw's APIKey wrapper as if it were the key's and prints null:\n\t%s",
						doc, i+1, scwKeyFallback, line)
				}
			}

			if doc != siteRunbookPath {
				continue
			}

			if len(matched) < minScwJSONLines {
				t.Errorf("%s holds %d line(s) that pipe `scw iam api-key get ... -o json` into jq, at line(s) %v, and this check needs at least %d: R0 reads the admin key's default project and owner with them, so a line that went missing is not a pass (issue #532)",
					doc, len(matched), matched, minScwJSONLines)
			}
		}
	})

	t.Run("R0's admin project and owner lines read through the wrapper", func(t *testing.T) {
		t.Parallel()

		for _, tc := range []struct {
			name   string
			prefix string
		}{
			{name: "ADMIN_PROJECT", prefix: adminProjectPrefix},
			{name: "ME", prefix: ownerPrefix},
		} {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				number, line, ok := runbookLineStartingWith(lines, tc.prefix)
				if !ok {
					t.Fatalf("%s holds no line starting %q: R0 reads the admin key's %s with it (issue #532)", siteRunbookPath, tc.prefix, tc.name)
				}

				if !strings.Contains(line, scwKeyFallback) {
					t.Errorf("%s:%d reads the admin key with %q but its jq filter has no %s, so a current scw's wrapped key prints null:\n\t%s",
						siteRunbookPath, number, tc.prefix, scwKeyFallback, line)
				}
			})
		}
	})

	// R0's PASS line reads a printed prefix as an owner, so a key scw could
	// not find - `"APIKey": null`, an error JSON, a new shape - must print
	// nothing at all, and the FAIL line next to it must name that shape: a
	// bare "application_id:" prefix is what the ME filter would otherwise
	// print, and today's FAIL line names only "user_id:" (reviewer finding 1,
	// auditor L1, issue #532).
	t.Run("R0's FAIL catalogue names a key scw could not find", func(t *testing.T) {
		t.Parallel()

		number, failLine, ok := runbookLineStartingWith(lines, failEmptyPrefix)
		if !ok {
			t.Fatalf("%s holds no line starting %q: it is R0's catalogue of the values a failed scw call prints (issue #532)", siteRunbookPath, failEmptyPrefix)
		}

		const missingKey = "application_id:"
		if !strings.Contains(failLine, missingKey) {
			t.Errorf("%s:%d lists the empty values a failed scw call prints and does not name %q: when scw does not find the key, the ME line's other branch prints %q alone, and a FAIL line that does not name it reads as a pass:\n\t%s",
				siteRunbookPath, number, missingKey, missingKey, failLine)
		}

		number, meLine, ok := runbookLineStartingWith(lines, ownerPrefix)
		if !ok {
			t.Fatalf("%s holds no line starting %q: R0 reads the admin key's owner with it (issue #532)", siteRunbookPath, ownerPrefix)
		}

		const printsNothing = "empty"
		if !strings.Contains(meLine, printsNothing) {
			t.Errorf("%s:%d prints a prefix even when the key has neither a user_id nor an application_id, so a key scw could not find reads as an owner; its jq filter must print nothing (jq's `empty`) in that case:\n\t%s",
				siteRunbookPath, number, meLine)
		}
	})

	// The check above is only worth its claims if the lines it skips are
	// skipped for a reason: R6 pipes the new key's JSON into the password
	// manager, not jq, the runbook's aws commands are not scw's, and another
	// scw command's output has no APIKey wrapper to fall back from.
	t.Run("only scw api-key JSON piped into jq is a filter", func(t *testing.T) {
		t.Parallel()

		for _, tc := range []struct {
			name string
			line string
			want bool
		}{
			{
				name: "R0's jq line",
				line: `ADMIN_PROJECT="$(scw iam api-key get "$SCW_ACCESS_KEY" -o json | jq -r .default_project_id)"`,
				want: true,
			},
			{
				name: "-o=json",
				line: `ME="$(scw iam api-key get "$SCW_ACCESS_KEY" -o=json | jq -r .user_id)"`,
				want: true,
			},
			{
				name: "-ojson",
				line: `ME="$(scw iam api-key get "$SCW_ACCESS_KEY" -ojson | jq -r .user_id)"`,
				want: true,
			},
			{
				name: "--output json",
				line: `ME="$(scw iam api-key get "$SCW_ACCESS_KEY" --output json | jq -r .user_id)"`,
				want: true,
			},
			{
				name: "--output=json",
				line: `ME="$(scw iam api-key get "$SCW_ACCESS_KEY" --output=json | jq -r .user_id)"`,
				want: true,
			},
			{
				name: "another scw command's JSON piped into jq",
				line: `scw object bucket get "$B" -o json | jq -r .endpoint`,
				want: false,
			},
			{
				name: "R6's password manager line",
				line: `# Or pipe it straight in, so the secret never reaches the screen: scw iam api-key create ... -o json | <your password manager's CLI>`,
				want: false,
			},
			{
				name: "an aws JSON line piped into jq",
				line: `aws s3api list-objects-v2 --bucket "$B" --output json | jq -c --slurpfile known "$W/known-keys.json" '[.Contents[]?.Key] | unique - $known[0]'   # PASS: []`,
				want: false,
			},
			{
				name: "scw JSON written to a file, not piped into jq",
				line: `scw iam api-key list -o json > "$W/keys.json"`,
				want: false,
			},
			{
				name: "a comment naming the pattern in prose, not a command",
				line: "The scw `-o json` output goes to jq; nothing to check.",
				want: false,
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				if got := scwAPIKeyJSONPipedToJQ.MatchString(tc.line); got != tc.want {
					t.Errorf("scwAPIKeyJSONPipedToJQ.MatchString(%q) = %t, want %t", tc.line, got, tc.want)
				}
			})
		}
	})
}

// scwJSONDocs returns the documents the filter check reads: the site runbook
// first, then every guide, whose names come back sorted.
func scwJSONDocs(t *testing.T, fsys fs.FS) []string {
	t.Helper()

	guides, err := fs.Glob(fsys, guidesGlob)
	if err != nil {
		t.Fatalf("globbing %s: %v", guidesGlob, err)
	}
	if len(guides) == 0 {
		t.Fatalf("%s matches no guide, so the filter check would pass on the runbook alone: point it at where the guides are", guidesGlob)
	}

	return append([]string{siteRunbookPath}, guides...)
}

// runbookLineStartingWith returns the 1-based number and the text of the first
// line of lines that starts with prefix, ignoring leading whitespace.
func runbookLineStartingWith(lines []string, prefix string) (number int, line string, ok bool) {
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimLeft(line, " \t"), prefix) {
			return i + 1, line, true
		}
	}
	return 0, "", false
}
