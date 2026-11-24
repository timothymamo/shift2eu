// Pins that the site runbook reads the JSON of a `scw ... -o json` command
// through the wrapper current scw releases print (issue #532): R0's
// `ADMIN_PROJECT` and `ME` lines take the admin key's default project and owner
// with jq filters that start from the key itself.
//
// A current scw CLI answers `scw iam api-key get` with the key inside an
// `APIKey` field ({"APIKey": {"user_id": ..., "default_project_id": ...}, ...}),
// older releases print the key unwrapped. A filter of the key's own fields,
// `.default_project_id`, reads nothing from the wrapper and makes jq print
// `null`, which R0's PASS line reads as a failed call: every filter that a
// `scw ... -o json` line pipes into jq therefore starts by falling back from
// the wrapper to the key, `(.APIKey // .)`, and works with both shapes.
//
// The check reads the runbook's lines as the repository holds them, so it needs
// neither the scw CLI nor jq: it asserts on the filters' text alone. The line
// that pipes `-o json` into something other than jq (R6's password manager CLI)
// is not a jq filter and is not matched.
package docs_test

import (
	"os"
	"strings"
	"testing"
)

const (
	// siteRunbookPath is the site runbook the R0 lines live in.
	siteRunbookPath = "docs/site-runbook.md"

	// scwKeyFallback is the jq path every filter over a `scw ... -o json` line
	// starts with: scw's APIKey wrapper when the CLI prints one, else the key
	// itself (issue #532).
	scwKeyFallback = "(.APIKey // .)"

	// adminProjectPrefix and ownerPrefix are R0's two lines that read the
	// admin's own API key, the two commands whose filter this check pins.
	adminProjectPrefix = `ADMIN_PROJECT="$(scw iam api-key get`
	ownerPrefix        = `ME="$(scw iam api-key get`

	// minScwJSONLines is how many `scw ... -o json | jq` lines the runbook
	// must keep: R0's two, and never fewer, so a deleted line cannot make the
	// filter check pass on nothing.
	minScwJSONLines = 2
)

// TestRunbookReadsTheScwAPIKeyJSONThroughItsWrapper pins that every runbook
// line that pipes `scw ... -o json` into jq reads the key through
// `(.APIKey // .)` (issue #532), and that the two lines R0 reads the admin's
// key with are there and do so.
func TestRunbookReadsTheScwAPIKeyJSONThroughItsWrapper(t *testing.T) {
	t.Parallel()

	lines := strings.Split(readDoc(t, os.DirFS(repoRoot), siteRunbookPath), "\n")

	t.Run("every scw JSON line piped into jq reads through the wrapper", func(t *testing.T) {
		t.Parallel()

		var matched []int

		for i, line := range lines {
			if !scwJSONPipedToJQ(line) {
				continue
			}

			matched = append(matched, i+1)

			if !strings.Contains(line, scwKeyFallback) {
				t.Errorf("%s:%d pipes scw's -o json output into jq without %s, so the filter reads a field of scw's APIKey wrapper as if it were the key's and prints null:\n\t%s",
					siteRunbookPath, i+1, scwKeyFallback, line)
			}
		}

		if len(matched) < minScwJSONLines {
			t.Errorf("%s holds %d line(s) that pipe `scw ... -o json` into jq, at line(s) %v, and this check needs at least %d: R0 reads the admin key's default project and owner with them, so a line that went missing is not a pass (issue #532)",
				siteRunbookPath, len(matched), matched, minScwJSONLines)
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

				number, line, ok := runbookCommandLine(lines, tc.prefix)
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

	// The check above is only worth its claims if the lines it skips are
	// skipped for a reason: R6 pipes the new key's JSON into the password
	// manager, not jq, and the runbook's aws commands are not scw's.
	t.Run("only scw JSON piped into jq is a filter", func(t *testing.T) {
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

				if got := scwJSONPipedToJQ(tc.line); got != tc.want {
					t.Errorf("scwJSONPipedToJQ(%q) = %t, want %t", tc.line, got, tc.want)
				}
			})
		}
	})
}

// scwJSONPipedToJQ reports whether line runs a scw command whose `-o json`
// output is piped into jq: the commands whose filters this check reads. A line
// that pipes the JSON elsewhere, an aws command and prose about scw are not
// such a command.
func scwJSONPipedToJQ(line string) bool {
	return strings.Contains(line, "scw ") &&
		strings.Contains(line, "-o json") &&
		strings.Contains(line, "| jq")
}

// runbookCommandLine returns the 1-based number and the text of the first line
// that starts with prefix, ignoring leading whitespace.
func runbookCommandLine(lines []string, prefix string) (number int, line string, ok bool) {
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimLeft(line, " \t"), prefix) {
			return i + 1, line, true
		}
	}
	return 0, "", false
}
