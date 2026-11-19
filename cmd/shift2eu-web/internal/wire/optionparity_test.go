package wire_test

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/internal/web"
)

// The inventory both driving adapters assess in the option table: a sanitised
// fixture, read by the CLI from a file and by the page from the same bytes.
const optionInventoryPath = "../../../../test/e2e/testdata/inventory.json"

// optionCase is one set of choices both driving adapters must agree on: accepted
// as it is, or refused with the same message.
type optionCase struct {
	name string
	// targets are the page's chosen target keys, in order; the CLI is given them
	// as its one --target value, joined by commas. A nil slice is an empty
	// --target value, which both refuse.
	targets []string
	// regions are the chosen regions, in order; the CLI is given them as its
	// --region value.
	regions []web.Region
	// estimate is the page's estimate switch; false adds --no-estimate.
	estimate bool
	// schemaVersion is the page's JSON schema version; the CLI is given it as
	// --schema-version, which needs the JSON format.
	schemaVersion string
	// format is the CLI's --format value, or "" for the default Markdown report.
	// The page renders both downloads every time, so it is the one the case
	// compares.
	format string
}

// TestThePageAndTheCLIRefuseTheSameOptions pins that the option rules are the
// CLI's: one table of option sets runs through the CLI and through the page's
// adapter over the same wire Deps, and each set is accepted by both or refused
// by both with the same message, the CLI's exit code 2 matching the page's
// KindUsage. For an accepted set the report the page downloads is the CLI's
// report, byte for byte (PRD #297).
func TestThePageAndTheCLIRefuseTheSameOptions(t *testing.T) {
	t.Parallel()

	document := readInventory(t, optionInventoryPath)
	inventoryPath := writeInventory(t, inventoryFileName, document)
	assessor := newAssessor(t)

	tests := []optionCase{
		{name: "the OVHcloud target", targets: []string{"ovhcloud"}, estimate: true},
		{name: "the Scaleway target", targets: []string{"scaleway"}, estimate: true},
		{name: "the UpCloud target", targets: []string{"upcloud"}, estimate: true},
		{name: "every target", targets: []string{"all"}, estimate: true},
		{name: "several targets", targets: []string{"ovhcloud", "upcloud"}, estimate: true},
		{
			name:     "a region for the selected target",
			targets:  []string{"scaleway"},
			regions:  []web.Region{{Target: "scaleway", Region: "nl-ams"}},
			estimate: true,
		},
		{
			name:     "a region for one of several targets",
			targets:  []string{"ovhcloud", "scaleway"},
			regions:  []web.Region{{Target: "scaleway", Region: "fr-par"}},
			estimate: true,
		},
		{name: "estimates on", targets: []string{"scaleway"}, estimate: true},
		{name: "estimates off", targets: []string{"scaleway"}, estimate: false},
		{
			name:          "the JSON report in schema version 2",
			targets:       []string{"scaleway"},
			estimate:      true,
			schemaVersion: "2",
			format:        jsonFormat,
		},
		{
			name:          "the JSON report in schema version 3",
			targets:       []string{"scaleway"},
			estimate:      true,
			schemaVersion: "3",
			format:        jsonFormat,
		},
		{
			name:     "the JSON report in the default schema version",
			targets:  []string{"scaleway"},
			estimate: true,
			format:   jsonFormat,
		},
		{name: "no target chosen", estimate: true},
		{name: "an unknown target", targets: []string{"nowhere"}, estimate: true},
		{name: "an empty target key", targets: []string{"scaleway", ""}, estimate: true},
		{name: "every target beside another", targets: []string{"all", "scaleway"}, estimate: true},
		{name: "a target given more than once", targets: []string{"scaleway", "scaleway"}, estimate: true},
		{
			name:     "a region for a target not selected",
			targets:  []string{"scaleway"},
			regions:  []web.Region{{Target: "upcloud", Region: "de-fra1"}},
			estimate: true,
		},
		{
			name:    "a region given for one target more than once",
			targets: []string{"ovhcloud", "scaleway"},
			regions: []web.Region{
				{Target: "scaleway", Region: "fr-par"},
				{Target: "scaleway", Region: "nl-ams"},
			},
			estimate: true,
		},
		{
			name:     "an unknown region",
			targets:  []string{"scaleway"},
			regions:  []web.Region{{Target: "scaleway", Region: "xx-nope"}},
			estimate: true,
		},
		{
			name:     "an empty region",
			targets:  []string{"scaleway"},
			regions:  []web.Region{{Target: "scaleway"}},
			estimate: true,
		},
		{
			name:          "an unknown schema version",
			targets:       []string{"scaleway"},
			estimate:      true,
			schemaVersion: "9",
			format:        jsonFormat,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			args := assessArgs(inventoryPath, tc)
			outcome := runCLI(t, args...)

			result, err := assessor.Assess(t.Context(), document, web.Options{
				Targets:       tc.targets,
				Regions:       tc.regions,
				Estimate:      tc.estimate,
				SchemaVersion: tc.schemaVersion,
				InventoryName: inventoryFileName,
			})

			if outcome.exit == 0 {
				if err != nil {
					t.Fatalf("shift2eu %s succeeded and the page refused the same options: %v",
						strings.Join(args, " "), err)
				}

				compareReport(t, tc, result, outcome.stdout)

				return
			}

			if outcome.exit != 2 {
				t.Fatalf("shift2eu %s = exit %d, want 2 or 0; message: %q",
					strings.Join(args, " "), outcome.exit, outcome.message)
			}

			if err == nil {
				t.Fatalf("shift2eu %s was refused with %q and the page accepted the same options; report: %+v",
					strings.Join(args, " "), outcome.message, result)
			}

			var usage *web.Error
			if !errors.As(err, &usage) || usage.Kind != web.KindUsage {
				t.Fatalf("the page refused the options the CLI refused with %q, but returned %v (%T), want a KindUsage *web.Error",
					outcome.message, err, err)
			}

			if usage.Error() != outcome.message {
				t.Errorf("the CLI was refused with %q and the page with %q, want the same message",
					outcome.message, usage.Error())
			}

			if len(result.Markdown) != 0 || len(result.JSON) != 0 || len(result.HTML) != 0 {
				t.Errorf("the page refused the options and still answered a report: %+v", result)
			}
		})
	}
}

// assessArgs returns the CLI's argument list for tc, the options the page is
// given as the same option set on the command line.
func assessArgs(inventoryPath string, tc optionCase) []string {
	args := []string{
		"assess",
		"--inventory", inventoryPath,
		"--target", strings.Join(tc.targets, ","),
	}

	if tc.format != "" {
		args = append(args, "--format", tc.format)
	}

	if tc.schemaVersion != "" {
		args = append(args, "--schema-version", tc.schemaVersion)
	}

	if len(tc.regions) > 0 {
		pairs := make([]string, 0, len(tc.regions))
		for _, region := range tc.regions {
			pairs = append(pairs, region.Target+"="+region.Region)
		}

		args = append(args, "--region", strings.Join(pairs, ","))
	}

	if !tc.estimate {
		args = append(args, "--no-estimate")
	}

	return args
}

// compareReport fails when the page's download differs from the CLI's report for
// the same options.
func compareReport(t *testing.T, tc optionCase, result web.Result, cliReport []byte) {
	t.Helper()

	got, want := result.Markdown, cliReport
	if tc.format == jsonFormat {
		got = result.JSON
	}

	if !bytes.Equal(want, got) {
		t.Errorf("the page's %s download differs from the CLI's report for the same options (-CLI +page):\n%s",
			formatName(tc.format), cmpDiff(want, got))
	}
}

// formatName names the format a case compares: the CLI's --format value, or
// Markdown for the default.
func formatName(format string) string {
	if format == "" {
		return markdownFormat
	}

	return format
}

// cmpDiff renders the first line two reports differ on, with both versions
// quoted, so a parity failure names one change.
func cmpDiff(want, got []byte) string {
	wantLines := strings.Split(string(want), "\n")
	gotLines := strings.Split(string(got), "\n")

	for i := 0; i < max(len(wantLines), len(gotLines)); i++ {
		var wantLine, gotLine string
		if i < len(wantLines) {
			wantLine = wantLines[i]
		}

		if i < len(gotLines) {
			gotLine = gotLines[i]
		}

		if wantLine != gotLine {
			return "line " + strconv.Itoa(i+1) + ":\n  CLI:  " + strconv.Quote(wantLine) +
				"\n  page: " + strconv.Quote(gotLine)
		}
	}

	return "the two reports hold the same lines but differ in their bytes"
}
