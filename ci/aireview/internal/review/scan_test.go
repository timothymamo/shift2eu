package review_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/timothymamo/shift2eu/ci/aireview/internal/review"
)

// goldenAdds returns the file section git writes for a path the reviewer leaves
// out that adds lines, from line 1 of its new side: a golden file of a testdata
// directory or the generated OpenAPI bundle, added whole.
func goldenAdds(path string, lines ...string) string {
	var section strings.Builder

	fmt.Fprintf(&section, "diff --git a/%s b/%s\nnew file mode 100644\n--- /dev/null\n+++ b/%s\n@@ -0,0 +1,%d @@\n",
		path, path, path, len(lines))

	for _, line := range lines {
		section.WriteString("+" + line + "\n")
	}

	return section.String()
}

// hasScanFinding reports whether body lists f as the review lists a finding of
// the model-free scan (issue #342): the file and the line of its new side in a
// code span, as the review shows a finding's path, and what was found on the
// same line.
func hasScanFinding(body string, f review.ScanFinding) bool {
	pattern := "`" + regexp.QuoteMeta(f.Path+":"+strconv.Itoa(f.Line)) + "`[^\\n]*" +
		regexp.QuoteMeta(string(f.Kind))

	return regexp.MustCompile(pattern).MatchString(body)
}

// TestScanLeftOutFindsRealDataInTheFilesLeftOut pins the model-free scan of
// issue #342: since the generated bundle and the golden files of testdata
// directories are left out of the diff the model reads, a real account ID, IP
// address, host name or key-like string committed in one of them would be
// reviewed by nobody. The scan reads the added lines of the left-out sections
// alone (a removed or a context line is not new data, and a reviewed file is the
// model's to read) and reports the file, the line of its new side and what it
// found, never the matched text: the review is public.
//
// A finding is a class, not a value, so the cases are the sanitised values
// CLAUDE.md rule 2 requires, which are no finding, beside real-looking ones,
// which are. The scan's list of files is FilterDiff's: the same paths in the
// same order, each with the number of lines it adds.
func TestScanLeftOutFindsRealDataInTheFilesLeftOut(t *testing.T) {
	t.Parallel()

	const (
		golden    = "testdata/report.golden"
		bundle    = "api/openapi.bundled.json"
		reviewed  = "internal/x/y.go"
		token     = "Zx9Qm2Lp7Rt4Vw8Yb3Nc6Kd1Hf5Jg0Sa2Bd4Ef6G"
		awsLikeID = "AKIAIOSFODNN7EXAMPLE"
	)

	cases := []struct {
		name         string
		diff         string
		wantFiles    []review.LeftOutFile
		wantFindings []review.ScanFinding
	}{
		{
			name:         "a real-looking account ID in a left-out golden is found",
			diff:         goldenAdds(golden, `"account_id": "987654321098"`),
			wantFiles:    []review.LeftOutFile{{Path: golden, Added: 1}},
			wantFindings: []review.ScanFinding{{Path: golden, Line: 1, Kind: review.ScanAccountID}},
		},
		{
			name:      "the documentation account ID is no finding",
			diff:      goldenAdds(golden, `"account_id": "123456789012"`),
			wantFiles: []review.LeftOutFile{{Path: golden, Added: 1}},
		},
		{
			name:      "the documentation IPv4 ranges are no finding",
			diff:      goldenAdds(golden, `"hosts": "192.0.2.10 198.51.100.7 203.0.113.5 0.0.0.0"`),
			wantFiles: []review.LeftOutFile{{Path: golden, Added: 1}},
		},
		{
			name:         "a real-looking IPv4 address is found",
			diff:         goldenAdds(golden, `"endpoint": "8.8.4.4"`),
			wantFiles:    []review.LeftOutFile{{Path: golden, Added: 1}},
			wantFindings: []review.ScanFinding{{Path: golden, Line: 1, Kind: review.ScanIPv4}},
		},
		{
			name: "the example hosts and the providers' own domains are no finding",
			diff: goldenAdds(golden, `"hosts": "shop.example.com www.example.com example.com evil.example `+
				`svc.example.test db.invalid api.localhost compute.googleapis.com storage.googleapis.com `+
				`aws.amazon.com amazonaws.com s3.eu-west-3.amazonaws.com azure.com microsoft.com `+
				`www.ovhcloud.com www.scaleway.com upcloud.com api.upcloud.com"`),
			wantFiles: []review.LeftOutFile{{Path: golden, Added: 1}},
		},
		{
			name:         "a host name outside the example domains is found",
			diff:         goldenAdds(golden, `"host": "db.acme-corp.io"`),
			wantFiles:    []review.LeftOutFile{{Path: golden, Added: 1}},
			wantFindings: []review.ScanFinding{{Path: golden, Line: 1, Kind: review.ScanHostname}},
		},
		{
			name:         "an access key ID is found",
			diff:         goldenAdds(golden, `"access_key_id": "`+awsLikeID+`"`),
			wantFiles:    []review.LeftOutFile{{Path: golden, Added: 1}},
			wantFindings: []review.ScanFinding{{Path: golden, Line: 1, Kind: review.ScanKey}},
		},
		{
			name:         "a long high-entropy token is found",
			diff:         goldenAdds(golden, `"token": "`+token+`"`),
			wantFiles:    []review.LeftOutFile{{Path: golden, Added: 1}},
			wantFindings: []review.ScanFinding{{Path: golden, Line: 1, Kind: review.ScanKey}},
		},
		{
			// The scan reads what the change adds: a value a line removes, or
			// one a context line keeps, is not new data in the repository.
			name: "a value on a removed or a context line is no finding",
			diff: "diff --git a/" + golden + " b/" + golden + "\n" +
				"--- a/" + golden + "\n" +
				"+++ b/" + golden + "\n" +
				"@@ -1,2 +1,2 @@\n" +
				"-account_id 987654321098\n" +
				" host db.acme-corp.io\n" +
				"+nothing of the kind\n",
			wantFiles: []review.LeftOutFile{{Path: golden, Added: 1}},
		},
		{
			// The model reads a reviewed file, so the scan leaves it alone:
			// its findings would only repeat what the review already says.
			name:      "the same value in a reviewed file is no finding",
			diff:      diffFile(reviewed, `account_id 987654321098 host db.acme-corp.io`),
			wantFiles: nil,
		},
		{
			name:         "the generated OpenAPI bundle is scanned like a golden file",
			diff:         goldenAdds(bundle, `"account_id": "987654321098"`),
			wantFiles:    []review.LeftOutFile{{Path: bundle, Added: 1}},
			wantFindings: []review.ScanFinding{{Path: bundle, Line: 1, Kind: review.ScanAccountID}},
		},
		{
			// The repository's own committed inventory goldens name their
			// volumes by a UUID-like id whose last group is twelve digits:
			// that is no account ID, and the regression over every golden file
			// (TestTheScanFindsNothingInTheCommittedGoldens) holds it too.
			name:      "a twelve-digit group inside a hyphenated id is no account ID",
			diff:      goldenAdds(golden, `"id": "pvc-00000000-0000-0000-0000-000000000001"`),
			wantFiles: []review.LeftOutFile{{Path: golden, Added: 1}},
		},
		{
			// A report's taxonomy categories read like host names, and the
			// committed goldens are full of them.
			name: "a taxonomy category is no host name",
			diff: goldenAdds(golden, `compute.vm storage.object messaging.queue database.postgresql `+
				`serverless.function compute.baremetal rdb.standard kubernetes.managed`),
			wantFiles: []review.LeftOutFile{{Path: golden, Added: 1}},
		},
		{
			// The bundle's JSON pointers are long runs of the base64 alphabet.
			name:      "a JSON pointer of the bundle is no key-like string",
			diff:      goldenAdds(bundle, `"$ref": "#/components/headers/Access-Control-Allow-Headers"`),
			wantFiles: []review.LeftOutFile{{Path: bundle, Added: 1}},
		},
		{
			name: "every finding names its own line of the new side, in the diff's order",
			diff: goldenAdds(golden,
				`"account_id": "987654321098"`,
				`"note": "nothing of the kind"`,
				`"endpoint": "8.8.4.4"`),
			wantFiles: []review.LeftOutFile{{Path: golden, Added: 3}},
			wantFindings: []review.ScanFinding{
				{Path: golden, Line: 1, Kind: review.ScanAccountID},
				{Path: golden, Line: 3, Kind: review.ScanIPv4},
			},
		},
		{
			name: "each left-out file is counted, a deleted or binary one with no added line",
			diff: goldenAdds("testdata/a.golden", "one", "two", "three") +
				deletedFile("testdata/old.golden", "gone") +
				binaryFile("testdata/image.golden"),
			wantFiles: []review.LeftOutFile{
				{Path: "testdata/a.golden", Added: 3},
				{Path: "testdata/image.golden", Added: 0},
				{Path: "testdata/old.golden", Added: 0},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := review.ScanLeftOut(tc.diff)

			if diff := cmp.Diff(tc.wantFiles, got.Files, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("ScanLeftOut's files (-want +got):\n%s", diff)
			}

			if diff := cmp.Diff(tc.wantFindings, got.Findings, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("ScanLeftOut's findings (-want +got):\n%s", diff)
			}

			// The scan reads the sections the reviewer left out and no others:
			// its list is FilterDiff's, path for path and in the same order, so
			// the note and the prompt name the same files whatever it finds.
			_, excluded := review.FilterDiff(tc.diff)

			paths := make([]string, 0, len(got.Files))
			for _, file := range got.Files {
				paths = append(paths, file.Path)
			}

			if !slices.Equal(excluded, paths) {
				t.Errorf("ScanLeftOut names the files %v, want FilterDiff's left-out files %v: the scan reads exactly the files the review did not", paths, excluded)
			}

			for _, finding := range got.Findings {
				if finding.Line < 1 {
					t.Errorf("ScanLeftOut reports %+v on line %d: a finding names the line of the file's new side it was found on (issue #342)", finding, finding.Line)
				}
			}
		})
	}
}

// TestTheReviewReportsWhatTheScanFoundInTheFilesLeftOut pins that the scan's
// findings reach the review comment (issue #342): each names the file and the
// line of its new side in the note's escaped code span and what was found, so
// the maintainer can go and look at the file the AI review never read. The
// matched text itself is never printed, not even in part: the review is public,
// and the value is the thing that must not be published.
func TestTheReviewReportsWhatTheScanFoundInTheFilesLeftOut(t *testing.T) {
	t.Parallel()

	const golden = "testdata/report.golden"

	// Every line plants one of the four classes: an account ID, an IPv4
	// address, a host name and a key.
	planted := []string{
		`"account_id": "987654321098"`,
		`"host": "db.acme-corp.io"`,
		`"endpoint": "8.8.4.4"`,
		`"access_key_id": "AKIAIOSFODNN7EXAMPLE"`,
	}

	raw := goldenAdds(golden, planted...)

	got := review.RenderFiltered(headSHA,
		review.Answer{Summary: renderSummary, Findings: []review.Finding{renderNoPosition}},
		review.EventComment, "", review.ScanLeftOut(raw))

	wantFindings := []review.ScanFinding{
		{Path: golden, Line: 1, Kind: review.ScanAccountID},
		{Path: golden, Line: 2, Kind: review.ScanHostname},
		{Path: golden, Line: 3, Kind: review.ScanIPv4},
		{Path: golden, Line: 4, Kind: review.ScanKey},
	}

	previous := -1

	for _, finding := range wantFindings {
		if !hasScanFinding(got.Body, finding) {
			t.Errorf("the review does not list %+v: a finding of the scan names the file, the line of its new side and what was found (issue #342); body:\n%s",
				finding, tail(got.Body, 4_000))

			continue
		}

		at := strings.Index(got.Body, "`"+finding.Path+":"+strconv.Itoa(finding.Line)+"`")
		if at < previous {
			t.Errorf("the review lists %+v before the finding above it: the scan's findings keep the diff's order; body:\n%s",
				finding, tail(got.Body, 4_000))
		}

		previous = at
	}

	// The comment is public: the matched value never reaches it, in whole or in
	// part, so a review can never publish the data it warns about.
	for _, value := range []string{"987654321098", "db.acme-corp.io", "8.8.4.4", "AKIAIOSFODNN7EXAMPLE"} {
		if strings.Contains(got.Body, value) {
			t.Errorf("the review's body holds %q, the value a scan finding matched: a finding names the file, the line and the class, never the text (issue #342); body:\n%s",
				value, tail(got.Body, 4_000))
		}
	}

	// The note still names the file, now with the lines it adds.
	if want := "- `" + golden + "` (+4 lines)"; !strings.Contains(got.Body, want) {
		t.Errorf("the note holds no %q: every left-out file shows the lines it adds (issue #342); body:\n%s",
			want, tail(got.Body, 4_000))
	}

	// The scan's findings are the note's, not the model's: the model's own
	// finding is still where Render puts it, and the scan adds none of its own
	// to the answer.
	if !strings.Contains(got.Body, renderNoPosition.Title) {
		t.Errorf("the review drops the model's finding %q: the scan adds a note about the files left out, it changes no finding of the review; body:\n%s",
			renderNoPosition.Title, tail(got.Body, 4_000))
	}

	assertBodyWithinLimit(t, got.Body)
}

// TestTheScanFindingsAreCapped pins that a left-out file cannot grow the review
// without bound (issue #342): the review lists the first review.MaxScanFindings
// findings of the scan and says how many more it found, as the note caps its
// list of paths, so a pull request that commits a thousand values costs a
// bounded review.
func TestTheScanFindingsAreCapped(t *testing.T) {
	t.Parallel()

	// More findings than the cap, each in a file of its own, so the note's own
	// list of paths is short of its own cap of 50 and only the scan's is cut.
	const total = 30

	var raw strings.Builder

	paths := make([]string, 0, total)

	for i := range total {
		// No digit in a path: the test reads a count of findings back from the
		// body, and a path holding one would be a number of its own there.
		path := fmt.Sprintf("testdata/scan-%c%c.golden", 'a'+i/26, 'a'+i%26)
		paths = append(paths, path)

		raw.WriteString(goldenAdds(path, `"endpoint": "8.8.4.4"`))
	}

	leftOut := review.ScanLeftOut(raw.String())

	if len(leftOut.Findings) != total {
		t.Fatalf("ScanLeftOut found %d findings in %d files, want %d: the scan reports every finding, and the review caps what it lists",
			len(leftOut.Findings), total, total)
	}

	if review.MaxScanFindings != 20 {
		t.Errorf("MaxScanFindings = %d, want 20: the review lists the first 20 findings of the scan (issue #342)", review.MaxScanFindings)
	}

	got := review.RenderFiltered(headSHA, review.Answer{Summary: renderSummary}, review.EventComment, "", leftOut)

	// A finding of the scan is the file and the line in a code span: the note's
	// own list of the same files shows no line, so counting them counts the
	// scan's list alone.
	if listed := strings.Count(got.Body, ":1`"); listed != review.MaxScanFindings {
		t.Errorf("the review lists %d of the scan's %d findings, want the first %d: a pull request cannot grow the review past the cap (issue #342); body:\n%s",
			listed, total, review.MaxScanFindings, tail(got.Body, 4_000))
	}

	// The review says how many findings it does not list, so its list never
	// reads as all of them.
	more := regexp.MustCompile(`(?m)^.*\bmore\b.*$`)

	found := false

	for _, line := range more.FindAllString(got.Body, -1) {
		if regexp.MustCompile(`\b` + strconv.Itoa(total-review.MaxScanFindings) + `\b`).MatchString(line) {
			found = true
		}
	}

	if !found {
		t.Errorf("the review holds no line that says how many of the scan's findings it does not list (want %d more): a capped list is counted (issue #342); body:\n%s",
			total-review.MaxScanFindings, tail(got.Body, 4_000))
	}

	assertBodyWithinLimit(t, got.Body)
}

// TestTheScanFindsNothingInTheCommittedGoldens pins the scan against the data
// this repository commits (issue #342): every golden file of a testdata
// directory and the generated OpenAPI bundle, with every line of it treated as
// added, so the whole of every one of them is read. CLAUDE.md rule 2 keeps them
// sanitised (123456789012, 192.0.2.0/24, example.com), so the scan is quiet on
// them, and a value it reports is real data committed by mistake, which no model
// reads.
//
// The files are read relative to this package's directory, which is four levels
// below the repository root, and the rule for which of them is left out is the
// reviewer's own (issue #324), restated here so the test names the files it
// scans itself rather than trusting the code it checks.
func TestTheScanFindsNothingInTheCommittedGoldens(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..", "..", "..")

	paths := committedLeftOutFiles(t, root)

	if len(paths) < 2 {
		t.Fatalf("found %d left-out files under %s (%v), want the repository's golden files and its bundle: the test scans nothing", len(paths), root, paths)
	}

	for _, path := range paths {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}

		lines := strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")

		leftOut := review.ScanLeftOut(goldenAdds(path, lines...))

		if len(leftOut.Files) != 1 {
			t.Fatalf("ScanLeftOut left %d sections out of the one-file diff of %s, want 1: the file is a left-out one", len(leftOut.Files), path)
		}

		for _, finding := range leftOut.Findings {
			t.Errorf("%s line %d: the scan reports %q in a file this repository commits, and CLAUDE.md rule 2 keeps its fixtures sanitised (issue #342)",
				path, finding.Line, finding.Kind)
		}
	}
}

// committedLeftOutFiles returns the paths, relative to root and with forward
// slashes, of the files the reviewer leaves out of the diff it reads (issue
// #324): api/openapi.bundled.json, and every file ending .golden with a
// directory named exactly testdata among its parents.
func committedLeftOutFiles(t *testing.T, root string) []string {
	t.Helper()

	var paths []string

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			if entry.Name() == ".git" {
				return fs.SkipDir
			}

			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		rel = filepath.ToSlash(rel)

		if rel == "api/openapi.bundled.json" || (strings.HasSuffix(rel, ".golden") &&
			slices.Contains(parentsOf(rel), "testdata")) {
			paths = append(paths, rel)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	slices.Sort(paths)

	return paths
}

// parentsOf returns the parent elements of a slash-separated path.
func parentsOf(path string) []string {
	elements := strings.Split(path, "/")

	return elements[:len(elements)-1]
}
