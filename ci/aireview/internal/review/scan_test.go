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

// gitBinaryPatchFile returns the section git writes for a binary file when it
// patches it instead of naming it, as "git diff --binary" does: no "---",
// "+++", hunk or "Binary files" line, and the patch's own base85 lines.
func gitBinaryPatchFile(path string) string {
	return "diff --git a/" + path + " b/" + path + "\n" +
		"index 1111111..2222222 100644\n" +
		"GIT binary patch\n" +
		"literal 12\n" +
		"TcmZ?wbhEHbRA=!R0RR91\n" +
		"\n" +
		"literal 0\n" +
		"HcmV?d00001\n" +
		"\n"
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
			// A provider's domain is no hiding place (issue #342, round 4 S1):
			// left out whole, it hid the value most likely to be real, a
			// customer's own bucket, database or account host. Each of these has
			// more than one label below the provider's domain, so the part the
			// provider owns is not the whole of it.
			name: "a host name more than one label below a provider's domain is found",
			diff: goldenAdds(golden,
				`"db": "prod-db.c9akciq32.eu-west-1.rds.amazonaws.com"`,
				`"blob": "mycorp.blob.core.windows.net"`,
				`"registry": "123456789012.dkr.ecr.eu-west-1.amazonaws.com"`,
				`"bucket": "myproject-prod.storage.googleapis.com"`),
			wantFiles: []review.LeftOutFile{{Path: golden, Added: 4}},
			wantFindings: []review.ScanFinding{
				{Path: golden, Line: 1, Kind: review.ScanHostname},
				{Path: golden, Line: 2, Kind: review.ScanHostname},
				{Path: golden, Line: 3, Kind: review.ScanHostname},
				{Path: golden, Line: 4, Kind: review.ScanHostname},
			},
		},
		{
			// A provider's own endpoint is no customer's data: the apex, exactly
			// one label below it (the service), and for amazonaws.com also
			// <service>.<region>, a region of the AWS shape. Anything deeper is
			// a name a customer chose (S1).
			name: "a provider's apex, one label below it and an amazonaws region service are no finding",
			diff: goldenAdds(golden, `"hosts": "compute.googleapis.com storage.googleapis.com `+
				`sqladmin.googleapis.com www.scaleway.com www.ovhcloud.com aws.amazon.com amazonaws.com `+
				`s3.eu-west-1.amazonaws.com ec2.us-east-2.amazonaws.com upcloud.com api.upcloud.com `+
				`github.com api.github.com"`),
			wantFiles: []review.LeftOutFile{{Path: golden, Added: 1}},
		},
		{
			// RFC 2606's example domains are the sanitised values CLAUDE.md
			// rule 2 asks the fixtures for, so they stay no finding at any depth
			// (S1).
			name:      "an example domain is no finding at any depth",
			diff:      goldenAdds(golden, `"hosts": "shop.example.com a.b.example.com deep.a.b.example.org www.example.net"`),
			wantFiles: []review.LeftOutFile{{Path: golden, Added: 1}},
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
			// Issue #342, round 4 S3: a secret access key's alphabet holds '/'
			// and '+', so a run of letters and digits alone split the key into
			// three short pieces and reported none of them. Both forms are the
			// AWS documentation's example key, whose '/'-less neighbour splits.
			name: "an AWS secret access key with a slash or a plus is found",
			diff: goldenAdds(golden,
				`"secret_access_key": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"`,
				`"secret_access_key_plus": "wJalrXUtnFEMI+K7MDENG+bPxRfiCYEXAMPLEKEY"`),
			wantFiles: []review.LeftOutFile{{Path: golden, Added: 2}},
			wantFindings: []review.ScanFinding{
				{Path: golden, Line: 1, Kind: review.ScanKey},
				{Path: golden, Line: 2, Kind: review.ScanKey},
			},
		},
		{
			// Widening the key's alphabet must not turn a path into a secret:
			// a run that begins or ends with '/' is a path or a JSON pointer,
			// not a key, however long it is and whatever it holds (S3). The
			// bundle's schema references are over 32 characters long and hold
			// an upper-case letter, a lower-case letter and a digit.
			name: "a long JSON pointer or a repository path is no key-like string",
			diff: goldenAdds(bundle,
				`"$ref": "#/components/schemas/AssessmentReportV2"`,
				`"note": "internal/adapter/target/scaleway/testdata"`),
			wantFiles: []review.LeftOutFile{{Path: bundle, Added: 2}},
		},
		{
			// Issue #342, round 4 S-A: a relative path, a documentation URL and
			// an import path are runs of the key's own alphabet, over 32 long
			// and holding an upper-case letter, a lower-case letter and a digit,
			// so the rule that begins and ends with a letter or a digit reported
			// them as secret access keys. A key is exactly forty characters
			// long; a longer or a shorter run is no key, whatever it holds. The
			// two hosts of these paths are a tool's own domain, so the host-name
			// rule stays quiet about them too (api.github.com is one label below
			// github.com).
			name: "a relative path, a repository URL and an import path are no key-like string",
			diff: goldenAdds(golden,
				`"note": "internal/adapter/report/markdown/testdata/Report1"`,
				`"doc": "https://github.com/timothymamo/shift2eu/blob/main/docs/ADR0033x"`,
				`"import": "golang.org/x/tools/gopls/internal/LongName12Here"`),
			wantFiles: []review.LeftOutFile{{Path: golden, Added: 3}},
		},
		{
			// A real secret access key may begin with '/' or end with '+', and
			// the rule that a run beginning or ending with '/' is a path missed
			// both (S-A). Each of these is exactly the forty characters of the
			// AWS documentation's example key, with a letter replaced by the
			// separator: the one begins with '/', the other ends with '+'.
			name: "a secret access key that begins with a slash or ends with a plus is found",
			diff: goldenAdds(golden,
				`"secret_access_key": "/wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKE"`,
				`"secret_access_key_plus": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKE+"`),
			wantFiles: []review.LeftOutFile{{Path: golden, Added: 2}},
			wantFindings: []review.ScanFinding{
				{Path: golden, Line: 1, Kind: review.ScanKey},
				{Path: golden, Line: 2, Kind: review.ScanKey},
			},
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
			// Issue #342, round 4 S2: a '.' or a '/' only joins a run of digits
			// into a longer number when a digit is on the far side of the
			// separator, as in a decimal number. Beside a host name or a bucket
			// path the run is a value of its own, and hiding it was the likeliest
			// way to miss a real account ID. The documentation ID of the second
			// host names no account, so its line is the host's finding alone.
			name: "an account ID beside a dot or a slash is found",
			diff: goldenAdds(golden,
				`"registry": "210987654321.dkr.ecr.eu-west-1.amazonaws.com"`,
				`"uri": "s3://bucket/210987654321/x"`),
			wantFiles: []review.LeftOutFile{{Path: golden, Added: 2}},
			wantFindings: []review.ScanFinding{
				{Path: golden, Line: 1, Kind: review.ScanAccountID},
				{Path: golden, Line: 1, Kind: review.ScanHostname},
				{Path: golden, Line: 2, Kind: review.ScanAccountID},
			},
		},
		{
			// A digit on the far side of the '.' makes the run part of a
			// decimal number, which is no account ID (S2).
			name:      "a twelve-digit group inside a decimal number is no account ID",
			diff:      goldenAdds(golden, `"ratio": 1.210987654321`, `"fraction": 210987654321.5`),
			wantFiles: []review.LeftOutFile{{Path: golden, Added: 2}},
		},
		{
			// Issue #342, round 4 N-A: only a '.' joins a run of digits into a
			// longer number, because a decimal number is the one thing the
			// separator hides. A '/' beside the run is no hiding place: beside a
			// year in an object's path, or beside another number, the run is an
			// account ID, and the digit on the far side made the scan skip it.
			name: "an account ID beside a slash and another digit is found",
			diff: goldenAdds(golden,
				`"uri": "s3://logs/2024/210987654321/x"`,
				`"v": "1/210987654321"`),
			wantFiles: []review.LeftOutFile{{Path: golden, Added: 2}},
			wantFindings: []review.ScanFinding{
				{Path: golden, Line: 1, Kind: review.ScanAccountID},
				{Path: golden, Line: 2, Kind: review.ScanAccountID},
			},
		},
		{
			// The one run of digits beside a '/' that is no account ID is a
			// JSON pointer's, which references the document rather than naming a
			// value (N-A, S2). Its element here follows a year, so a digit is on
			// the far side of the '/' too, as in the case above.
			name:      "a twelve-digit run behind a slash and a digit inside a JSON pointer is no account ID",
			diff:      goldenAdds(bundle, `"$ref": "#/components/schemas/2024/210987654321"`),
			wantFiles: []review.LeftOutFile{{Path: bundle, Added: 1}},
		},
		{
			// A JSON pointer starting #/ is a reference into the bundle, not a
			// value: its elements are schema names and array indices of any
			// length, so a run of twelve digits there is no account ID (S2).
			name:      "a twelve-digit run inside a JSON pointer is no account ID",
			diff:      goldenAdds(bundle, `"$ref": "#/components/schemas/210987654321"`),
			wantFiles: []review.LeftOutFile{{Path: bundle, Added: 1}},
		},
		{
			// Issue #365: the scan's public TLDs hold info, app, dev, co, io
			// and ai, so a dotted identifier of Go code read as a host name. No
			// code is a host, and a golden that quotes code would have failed
			// the scan for no reason.
			name: "a dotted code name is no host name",
			diff: goldenAdds(golden,
				`slog.Info("the scan found nothing") cfg.App r.Dev errors.Is(err, errLeftOut)`),
			wantFiles: []review.LeftOutFile{{Path: golden, Added: 1}},
		},
		{
			// The TLDs a code name ends in carry real hosts too: narrowing the
			// rules for the code names must not quiet the scan about a name a
			// customer chose (issue #365).
			name: "a host name in a TLD a code name also uses is still found",
			diff: goldenAdds(golden,
				`"db": "db01.corp.info"`,
				`"shop": "shop.example.io"`,
				`"account_id": "210987654321"`),
			wantFiles: []review.LeftOutFile{{Path: golden, Added: 3}},
			wantFindings: []review.ScanFinding{
				{Path: golden, Line: 1, Kind: review.ScanHostname},
				{Path: golden, Line: 2, Kind: review.ScanHostname},
				{Path: golden, Line: 3, Kind: review.ScanAccountID},
			},
		},
		{
			// Issue #365: the code-name rule hides a real host when it reads a
			// "(" after the name as a call, and when it reads a mixed-case last
			// label alone as an exported Go identifier. A host name ends in a
			// public TLD whatever follows it, and only the whole run of code
			// decides whether it is a code name, so every one of these is a
			// host name (a golden that names a database, a customer's domain, a
			// capitalised host or a URL would otherwise pass the scan).
			name: "a host name a parenthesis or a mixed-case last label would have hidden is still found",
			diff: goldenAdds(golden,
				`"db01": "db01.corp.info(primary)"`,
				`"shop": "Shop.Acme.Com"`,
				`"corp": "Corp.Example.Info"`,
				`"db": "db01.corp.Info"`,
				`"mail": "https://Mail.Contoso.Net/x"`),
			wantFiles: []review.LeftOutFile{{Path: golden, Added: 5}},
			wantFindings: []review.ScanFinding{
				{Path: golden, Line: 1, Kind: review.ScanHostname},
				{Path: golden, Line: 2, Kind: review.ScanHostname},
				{Path: golden, Line: 3, Kind: review.ScanHostname},
				{Path: golden, Line: 4, Kind: review.ScanHostname},
				{Path: golden, Line: 5, Kind: review.ScanHostname},
			},
		},
		{
			// The narrowing the case above asks for must keep the code names
			// quiet: a selector on a receiver, and a call of one whose
			// parenthesis used to be the whole reason it was no host name
			// (issue #365).
			name:      "a chained selector and a call of one are no host name",
			diff:      goldenAdds(golden, `slog.Info("nothing") r.cfg.App x.cfg.Info("y")`),
			wantFiles: []review.LeftOutFile{{Path: golden, Added: 1}},
		},
		{
			// An old configuration file writes its host names all upper case,
			// which is no spelling of an exported Go identifier, so the
			// code-name rule must not quiet the scan about it (issue #365).
			name:         "an all-upper host name of an old configuration file is found",
			diff:         goldenAdds(golden, `"host": "DB01.CORP.INFO"`),
			wantFiles:    []review.LeftOutFile{{Path: golden, Added: 1}},
			wantFindings: []review.ScanFinding{{Path: golden, Line: 1, Kind: review.ScanHostname}},
		},
		{
			// This is a known and accepted false negative of the code-name
			// rule (issue #365): spelt like a Go selector, it cannot be told
			// from cfg.Info. Such a spelling is rare (configurations write
			// hosts all lower case or all upper case, and both are found),
			// and if it ever hides real data, the fix goes in the scan's
			// rules, never in a path allow list. The miss is broader than
			// that spelling: a two-label name such as contoso.Com or
			// acme.Dev reads as code too.
			name: "a host name in lower case with only its TLD in title case is an accepted miss",
			diff: goldenAdds(golden, `"db": "db.corp.Info" "mail": "mail.contoso.Com" `+
				`"site": "contoso.Com"`),
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
			// Issue #342, round 4 S4: a binary file has no line of text, so the
			// note's count said "(+0 lines)", which reads as nothing added,
			// though the file was neither reviewed nor scanned. The section git
			// writes for it is marked binary, so the scan says so.
			name: "each left-out file is counted, a binary one marked binary and a deleted one with no added line",
			diff: goldenAdds("testdata/a.golden", "one", "two", "three") +
				deletedFile("testdata/old.golden", "gone") +
				binaryFile("testdata/image.golden"),
			wantFiles: []review.LeftOutFile{
				{Path: "testdata/a.golden", Added: 3},
				{Path: "testdata/image.golden", Added: 0, Binary: true},
				{Path: "testdata/old.golden", Added: 0},
			},
		},
		{
			// git names a binary file in one line, or patches it as base85
			// when asked with --binary: both mark it binary (S4).
			name:      "a binary file git writes as a GIT binary patch is marked binary",
			diff:      gitBinaryPatchFile("testdata/logo.golden"),
			wantFiles: []review.LeftOutFile{{Path: "testdata/logo.golden", Added: 0, Binary: true}},
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
		`"secret_access_key": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"`,
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
		{Path: golden, Line: 5, Kind: review.ScanKey},
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
	// part, so a review can never publish the data it warns about. The secret
	// access key of issue #342 round 4 S3 is the value the scan could not see
	// before, so it is the one to watch here.
	for _, value := range []string{
		"987654321098", "db.acme-corp.io", "8.8.4.4", "AKIAIOSFODNN7EXAMPLE",
		"wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	} {
		if strings.Contains(got.Body, value) {
			t.Errorf("the review's body holds %q, the value a scan finding matched: a finding names the file, the line and the class, never the text (issue #342); body:\n%s",
				value, tail(got.Body, 4_000))
		}
	}

	// The note still names the file, now with the lines it adds.
	if want := "- `" + golden + "` (+5 lines)"; !strings.Contains(got.Body, want) {
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

	// The cap cuts the end of the scan's list: the review lists the first
	// MaxScanFindings findings the diff wrote, in that order, and none after
	// them, so a reader knows which findings the count of the rest covers.
	first, rest := paths[:review.MaxScanFindings], paths[review.MaxScanFindings:]

	previous := -1

	for _, path := range first {
		finding := review.ScanFinding{Path: path, Line: 1, Kind: review.ScanIPv4}

		if !hasScanFinding(got.Body, finding) {
			t.Errorf("the review lists no %s:%d of the scan's findings, of the %d it lists first: the cap keeps the scan's order (issue #342); body:\n%s",
				path, finding.Line, review.MaxScanFindings, tail(got.Body, 4_000))
		}

		if at := strings.Index(got.Body, "`"+path+":1`"); at < previous {
			t.Errorf("the review lists %s:%d before a finding the scan found earlier: the cap keeps the scan's order (issue #342); body:\n%s",
				path, finding.Line, tail(got.Body, 4_000))
		} else {
			previous = at
		}
	}

	for _, path := range rest {
		if hasScanFinding(got.Body, review.ScanFinding{Path: path, Line: 1, Kind: review.ScanIPv4}) {
			t.Errorf("the review lists the scan's finding in %q, which comes after the first %d: the cap drops the findings after them and counts them (issue #342); body:\n%s",
				path, review.MaxScanFindings, tail(got.Body, 4_000))
		}
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
//
// A failure here is a false positive of the scan or real data. A false positive
// is fixed in the scan's rules (scanLine and the rules it reads), never with an
// allow list of paths: a list of paths would have to grow with every fixture
// this repository commits, and a file moved onto it would hide real data as the
// scan went quiet about it (issue #365). Real data is removed from the fixture,
// as CLAUDE.md rule 2 asks.
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
			// A nested worktree (this repository keeps them under .worktrees/,
			// and the repository root holds one) has a .git entry of its own and
			// its own copy of every golden file: walking into it would scan
			// another branch's files and count them twice.
			if entry.Name() == ".git" || entry.Name() == ".worktrees" {
				return fs.SkipDir
			}

			if path != root {
				if _, err := os.Lstat(filepath.Join(path, ".git")); err == nil {
					return fs.SkipDir
				}
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
