package lint_test

import (
	"errors"
	"fmt"
	"go/build/constraint"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// negatableTags are the build tags every lint run sets, run.build-tags in
// .golangci.yml, which a build constraint must never negate.
// TestNegatableTagsMatchGolangci keeps the two equal. sitefront is the front's
// behaviour test (PRD #297 milestone 4, docs/adr/0045): only `make
// test-sitefront` runs it, and every lint run loads its files all the same, so
// the code that starts the container is never code no lint rule reads.
var negatableTags = map[string]bool{"browser": true, "e2e": true, "integration": true, "sitefront": true}

// constraintHides says why comment, the text of a comment above a file's
// package clause, could keep the file out of lint while a build compiles it.
// It returns "" when the comment is no //go:build or // +build line, or when
// the line cannot do that.
//
// Every lint run sets e2e and integration. A line that names them only
// un-negated holds with both set whenever it holds with either unset, so lint
// reads every file such a line lets a build compile. A line that negates one
// of them, such as !e2e, !(e2e && linux) or linux || !integration, is refused,
// also where the result would be harmless, such as !e2e || integration: never
// negate them (.golangci.yml).
func constraintHides(comment string) string {
	if !constraint.IsGoBuild(comment) && !constraint.IsPlusBuild(comment) {
		return ""
	}
	expr, err := constraint.Parse(comment)
	if err != nil {
		return "cannot be parsed, so test/lint cannot check it: " + err.Error()
	}
	if negatesLintTag(expr, false) {
		return "negates the e2e or integration tag; every lint run sets both (.golangci.yml), " +
			"so lint would never read a file a build compiles. Never negate them"
	}
	return ""
}

// negatesLintTag reports whether expr, under negated, holds a lint tag under
// an odd number of negations.
func negatesLintTag(expr constraint.Expr, negated bool) bool {
	switch e := expr.(type) {
	case *constraint.TagExpr:
		return negated && negatableTags[e.Tag]
	case *constraint.NotExpr:
		return negatesLintTag(e.X, !negated)
	case *constraint.AndExpr:
		return negatesLintTag(e.X, negated) || negatesLintTag(e.Y, negated)
	case *constraint.OrExpr:
		return negatesLintTag(e.X, negated) || negatesLintTag(e.Y, negated)
	}
	return false
}

func TestConstraintHides(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		comment string
		want    bool
	}{
		{name: "negates e2e", comment: "//go:build !e2e", want: true},
		{name: "negates integration", comment: "//go:build !integration", want: true},
		{name: "negates a conjunction", comment: "//go:build !(e2e && linux)", want: true},
		{name: "negates integration in a disjunction", comment: "//go:build linux || !integration", want: true},
		{name: "needs integration off", comment: "//go:build e2e && !integration", want: true},
		{name: "negates under a double negation", comment: "//go:build !(linux && !(!e2e))", want: true},
		{name: "harmless but negated", comment: "//go:build !e2e || integration", want: true},
		{name: "+build line", comment: "// +build !e2e", want: true},
		{name: "+build line with a list", comment: "// +build linux,!integration", want: true},
		{name: "cannot be parsed", comment: "//go:build !", want: true},
		{name: "e2e", comment: "//go:build e2e", want: false},
		{name: "integration", comment: "//go:build integration", want: false},
		{name: "double negation", comment: "//go:build !(linux && !e2e)", want: false},
		{name: "negates a platform", comment: "//go:build e2e && !windows", want: false},
		{name: "ignore", comment: "//go:build ignore", want: false},
		{name: "+build line naming e2e", comment: "// +build linux e2e", want: false},
		{name: "space after the slashes", comment: "// go:build !e2e", want: false},
		{name: "prose", comment: "// Never write //go:build !e2e.", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := constraintHides(tc.comment); (got != "") != tc.want {
				t.Errorf("constraintHides(%q) = %q, want a reason: %t", tc.comment, got, tc.want)
			}
		})
	}
}

// TestNegatableTagsMatchGolangci fails when negatableTags and run.build-tags in
// .golangci.yml differ, so a tag added to every lint run cannot be left out of
// the check that keeps it un-negated.
func TestNegatableTagsMatchGolangci(t *testing.T) {
	t.Parallel()

	// go test runs a test in its package's directory, test/lint.
	config, err := os.ReadFile(filepath.Join("..", "..", ".golangci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := runBuildTags(string(config))
	if err != nil {
		t.Fatalf(".golangci.yml: %v", err)
	}
	slices.Sort(got)
	want := make([]string, 0, len(negatableTags))
	for tag := range negatableTags {
		want = append(want, tag)
	}
	slices.Sort(want)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("negatableTags and run.build-tags in .golangci.yml differ (-negatableTags +run.build-tags):\n%s", diff)
	}
}

// runBuildTags returns the entries of run.build-tags in config, the text of
// .golangci.yml, written as a block sequence of plain tags below the key, as
// .golangci.yml writes them. It refuses every other form rather than guess at
// it: a flow list, a quoted or commented entry, a nested value, a repeated run
// or build-tags key, and a tab on any line of the file that is neither blank
// nor a comment, inside run.build-tags or not.
func runBuildTags(config string) ([]string, error) {
	var tags []string
	inRun, inTags, seenRun, seenTags := false, false, false, false
	for i, raw := range strings.Split(config, "\n") {
		num := i + 1
		raw = strings.TrimSuffix(raw, "\r")
		text := strings.TrimLeft(raw, " ")
		indent := len(raw) - len(text)
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		if strings.ContainsAny(raw, "\t") {
			return nil, fmt.Errorf("line %d: a tab", num)
		}
		switch {
		case indent == 0:
			inRun, inTags = text == "run:", false
			if inRun {
				if seenRun {
					return nil, fmt.Errorf("line %d: run is repeated", num)
				}
				seenRun = true
			}
		case !inRun:
		case indent == 2:
			inTags = strings.HasPrefix(text, "build-tags:")
			if !inTags {
				continue
			}
			if text != "build-tags:" {
				return nil, fmt.Errorf("line %d: %q, want build-tags with a block sequence below it", num, text)
			}
			if seenTags {
				return nil, fmt.Errorf("line %d: run.build-tags is repeated", num)
			}
			seenTags = true
		case !inTags:
		case indent == 4 && strings.HasPrefix(text, "- "):
			tag := strings.TrimPrefix(text, "- ")
			if !plainTag.MatchString(tag) {
				return nil, fmt.Errorf("line %d: %q is not a plain build tag", num, text)
			}
			tags = append(tags, tag)
		default:
			return nil, fmt.Errorf("line %d: %q in run.build-tags is not an entry this reader understands", num, text)
		}
	}
	if len(tags) == 0 {
		return nil, errors.New("no run.build-tags entries")
	}
	return tags, nil
}

// plainTag matches a build tag the go command accepts, written plain.
var plainTag = regexp.MustCompile(`^[A-Za-z0-9_.]+$`)

func TestRunBuildTags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  string
		want    []string
		wantErr bool
	}{
		{
			name:   "block sequence",
			config: "version: \"2\"\n\n# comment\nrun:\n  timeout: 5m\n  build-tags:\n    - e2e\n    # comment\n    - integration\n\nlinters:\n  default: standard\n",
			want:   []string{"e2e", "integration"},
		},
		{name: "no run", config: "linters:\n  build-tags:\n    - e2e\n", wantErr: true},
		{name: "no build-tags", config: "run:\n  timeout: 5m\n", wantErr: true},
		{name: "flow list", config: "run:\n  build-tags: [e2e, integration]\n", wantErr: true},
		{name: "quoted tag", config: "run:\n  build-tags:\n    - \"e2e\"\n", wantErr: true},
		{name: "commented tag", config: "run:\n  build-tags:\n    - e2e # probe\n", wantErr: true},
		{name: "nested value", config: "run:\n  build-tags:\n    - e2e\n      - integration\n", wantErr: true},
		{name: "repeated run", config: "run:\n  build-tags:\n    - e2e\nrun:\n  timeout: 5m\n", wantErr: true},
		{name: "repeated build-tags", config: "run:\n  build-tags:\n    - e2e\n  build-tags:\n    - integration\n", wantErr: true},
		{name: "indentless sequence", config: "run:\n  build-tags:\n  - e2e\n", wantErr: true},
		{name: "key with a trailing comment", config: "run:\n  build-tags: # x\n    - e2e\n", wantErr: true},
		{name: "tab", config: "run:\n  build-tags:\n\t- e2e\n", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := runBuildTags(tc.config)
			if (err != nil) != tc.wantErr {
				t.Fatalf("runBuildTags() error = %v, want an error: %t", err, tc.wantErr)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("runBuildTags() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
