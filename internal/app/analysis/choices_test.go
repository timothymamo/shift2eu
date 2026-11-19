package analysis_test

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/internal/app/analysis"
)

// supportedTargets are the target keys the option-rule tests check against,
// sorted as a driving adapter passes them.
var supportedTargets = []string{"ovhcloud", "scaleway", "upcloud"}

// checkChoiceError fails t unless err is nil when wantErr is "", or else has
// exactly the message wantErr and wraps analysis.ErrInvalidChoice.
func checkChoiceError(t *testing.T, err error, wantErr string) {
	t.Helper()

	if wantErr == "" {
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}

		return
	}

	if err == nil {
		t.Fatalf("err = nil, want %q", wantErr)
	}

	if got := err.Error(); got != wantErr {
		t.Errorf("err = %q, want %q", got, wantErr)
	}

	if !errors.Is(err, analysis.ErrInvalidChoice) {
		t.Errorf("errors.Is(err, ErrInvalidChoice) = false for %q", err)
	}
}

func TestSelectTargets(t *testing.T) {
	t.Parallel()

	const list = "supported targets: ovhcloud, scaleway, upcloud"

	tests := []struct {
		name    string
		given   []string
		want    []string
		wantErr string
	}{
		{name: "one target", given: []string{"scaleway"}, want: []string{"scaleway"}},
		{
			name:  "several targets come back sorted",
			given: []string{"upcloud", "ovhcloud"},
			want:  []string{"ovhcloud", "upcloud"},
		},
		{name: "all selects every target", given: []string{"all"}, want: supportedTargets},
		{name: "no target", given: nil, wantErr: "assess needs --target"},
		{
			name:    "an empty target quotes the whole choice",
			given:   []string{"scaleway", ""},
			wantErr: `--target "scaleway," has an empty target; ` + list,
		},
		{
			name:    "all beside another target",
			given:   []string{"scaleway", "all"},
			wantErr: `--target "all" cannot be combined with other targets; ` + list,
		},
		{
			name:    "all twice",
			given:   []string{"all", "all"},
			wantErr: `--target "all" cannot be combined with other targets; ` + list,
		},
		{
			name:    "a duplicate",
			given:   []string{"scaleway", "upcloud", "scaleway"},
			wantErr: `target "scaleway" given more than once; ` + list,
		},
		{
			name:    "an unknown target",
			given:   []string{"aws"},
			wantErr: `unknown target "aws"; ` + list,
		},
		{
			name:    "the first refusal wins",
			given:   []string{"aws", ""},
			wantErr: `unknown target "aws"; ` + list,
		},
		{
			name:    "a hostile key is quoted",
			given:   []string{"<script>\x1b"},
			wantErr: `unknown target "<script>\x1b"; ` + list,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := analysis.SelectTargets(tt.given, supportedTargets)
			checkChoiceError(t, err, tt.wantErr)

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("SelectTargets() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestSelectTargetsDoesNotShareSupported pins that the selection of all is a
// copy: changing it leaves the caller's supported keys as they were.
func TestSelectTargetsDoesNotShareSupported(t *testing.T) {
	t.Parallel()

	supported := []string{"ovhcloud", "scaleway"}

	got, err := analysis.SelectTargets([]string{analysis.AllTargets}, supported)
	if err != nil {
		t.Fatalf("SelectTargets: %v", err)
	}

	got[0] = "changed"

	if supported[0] != "ovhcloud" {
		t.Errorf("supported[0] = %q after changing the selection, want %q", supported[0], "ovhcloud")
	}
}

func TestSelectRegions(t *testing.T) {
	t.Parallel()

	selected := []string{"scaleway", "upcloud"}

	tests := []struct {
		name    string
		given   []analysis.RegionChoice
		want    map[string]string
		wantErr string
	}{
		{name: "no region", given: nil, want: nil},
		{
			name:  "one region per target",
			given: []analysis.RegionChoice{{Target: "scaleway", Region: "nl-ams"}, {Target: "upcloud", Region: "fi-hel1"}},
			want:  map[string]string{"scaleway": "nl-ams", "upcloud": "fi-hel1"},
		},
		{
			name:    "an empty region quotes the choice as written",
			given:   []analysis.RegionChoice{{Target: "scaleway", Text: " scaleway"}},
			wantErr: `--region " scaleway" is not <target>=<region>; selected targets: scaleway, upcloud`,
		},
		{
			name:    "an empty target",
			given:   []analysis.RegionChoice{{Region: "nl-ams", Text: "=nl-ams"}},
			wantErr: `--region "=nl-ams" is not <target>=<region>; selected targets: scaleway, upcloud`,
		},
		{
			name:    "an empty choice",
			given:   []analysis.RegionChoice{{Target: "scaleway", Region: "nl-ams", Text: "scaleway=nl-ams"}, {}},
			wantErr: `--region "" is not <target>=<region>; selected targets: scaleway, upcloud`,
		},
		{
			name:    "a target not selected",
			given:   []analysis.RegionChoice{{Target: "ovhcloud", Region: "gra"}},
			wantErr: `--region names target "ovhcloud", which --target does not select; selected targets: scaleway, upcloud`,
		},
		{
			name: "a target twice",
			given: []analysis.RegionChoice{
				{Target: "scaleway", Region: "nl-ams"},
				{Target: "scaleway", Region: "fr-par"},
			},
			wantErr: `--region names target "scaleway" more than once`,
		},
		{
			name: "the first refusal wins",
			given: []analysis.RegionChoice{
				{Target: "ovhcloud", Region: "gra"},
				{Target: "scaleway", Text: "scaleway"},
			},
			wantErr: `--region names target "ovhcloud", which --target does not select; selected targets: scaleway, upcloud`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := analysis.SelectRegions(tt.given, selected)
			checkChoiceError(t, err, tt.wantErr)

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("SelectRegions() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRegionChoiceError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		selected []string
		wantErr  string
	}{
		{name: "no error", err: nil, selected: []string{"scaleway"}},
		{name: "another error", err: errors.New("load inventory: broken"), selected: []string{"scaleway"}},
		{
			name: "a single-target run names its target",
			err: errors.Join(errors.New("wrapped"),
				&analysis.RegionError{Region: "fr-paris", Regions: []string{"fr-par", "nl-ams"}}),
			selected: []string{"scaleway"},
			wantErr:  `--region: target "scaleway": region "fr-paris" is not one of the target's regions: "fr-par", "nl-ams"`,
		},
		{
			name: "a comparison keeps the target the error names",
			err: &analysis.RegionError{
				Target: "upcloud", Region: "x", Regions: []string{"fi-hel1"},
			},
			selected: []string{"scaleway", "upcloud"},
			wantErr:  `--region: target "upcloud": region "x" is not one of the target's regions: "fi-hel1"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := analysis.RegionChoiceError(tt.err, tt.selected)
			checkChoiceError(t, err, tt.wantErr)

			if err != nil && !errors.Is(err, analysis.ErrUnknownRegion) {
				t.Errorf("errors.Is(err, ErrUnknownRegion) = false for %q", err)
			}
		})
	}
}

// TestRegionChoiceErrorLeavesTheRunErrorAlone pins that naming the target of
// a single-target run changes a copy, never the *RegionError RunWith returned.
func TestRegionChoiceErrorLeavesTheRunErrorAlone(t *testing.T) {
	t.Parallel()

	runErr := &analysis.RegionError{Region: "x", Regions: []string{"fr-par"}}

	if err := analysis.RegionChoiceError(runErr, []string{"scaleway"}); err == nil {
		t.Fatal("RegionChoiceError = nil, want an error")
	}

	if runErr.Target != "" {
		t.Errorf("runErr.Target = %q after RegionChoiceError, want \"\"", runErr.Target)
	}
}

func TestSelectFormat(t *testing.T) {
	t.Parallel()

	supported := []string{"json", "markdown"}

	tests := []struct {
		name    string
		format  string
		wantErr string
	}{
		{name: "markdown", format: "markdown"},
		{name: "json", format: "json"},
		{name: "unknown", format: "html", wantErr: `unknown format "html"; supported formats: json, markdown`},
		{name: "empty", format: "", wantErr: `unknown format ""; supported formats: json, markdown`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			checkChoiceError(t, analysis.SelectFormat(tt.format, supported), tt.wantErr)
		})
	}
}

func TestSelectSchemaVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		format  string
		value   string
		given   bool
		want    int
		wantErr string
	}{
		{name: "json without a version writes the default", format: "json", want: analysis.DefaultSchemaVersion},
		{name: "the default is 3", format: "json", want: 3},
		{name: "json version 2", format: "json", value: "2", given: true, want: 2},
		{name: "json version 3", format: "json", value: "3", given: true, want: 3},
		{
			name: "an unknown version", format: "json", value: "4", given: true,
			wantErr: `--schema-version: unknown schema version "4"; supported schema versions: 2, 3`,
		},
		{
			name: "an empty version given", format: "json", value: "", given: true,
			wantErr: `--schema-version: unknown schema version ""; supported schema versions: 2, 3`,
		},
		{name: "markdown has no version", format: "markdown", want: 0},
		{
			name: "a version for markdown", format: "markdown", value: "2", given: true,
			wantErr: `--schema-version applies only to --format json, not to --format "markdown"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := analysis.SelectSchemaVersion(tt.format, tt.value, tt.given)
			checkChoiceError(t, err, tt.wantErr)

			if got != tt.want {
				t.Errorf("SelectSchemaVersion() = %d, want %d", got, tt.want)
			}
		})
	}
}
