package wire_test

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/internal/web"
)

// The inventories the download parity runs over: every bundled example, which is
// discovered rather than counted here, and the two fixtures test/apiparity uses,
// one ordinary and one hostile.
const (
	examplesRoot = "../../../../examples"
	// parityFixture is the sanitised multi-category inventory the HTTP API's
	// parity test also reads.
	parityFixture = "../../../../test/e2e/testdata/inventory.json"
	// hostileFixture is the inventory whose every text value would be markup in
	// a renderer.
	hostileFixture = "../../../../test/e2e/testdata/inventory-hostile.json"
)

// downloadOption is one set of options a download is compared for: the target
// choice, the region per target, the estimate switch and the JSON report's
// schemaVersion.
type downloadOption struct {
	name     string
	targets  []string
	regions  []web.Region
	estimate bool
	// schemaVersion is the schemaVersion the JSON download is written in: "2", or
	// "" for the default. The Markdown download has no schemaVersion, and the CLI
	// refuses --schema-version beside another format, so it reaches the JSON
	// comparison alone.
	schemaVersion string
}

// downloadOptions are the option sets every inventory is downloaded with: one
// target, several and every target, with and without a region, and with and
// without estimates, and schema version 2 for one target and for every target.
func downloadOptions() []downloadOption {
	return []downloadOption{
		{name: "one target", targets: []string{"scaleway"}, estimate: true},
		{name: "several targets", targets: []string{"ovhcloud", "upcloud"}, estimate: true},
		{name: "every target", targets: []string{"all"}, estimate: true},
		{
			name:     "one target in a region",
			targets:  []string{"scaleway"},
			regions:  []web.Region{{Target: "scaleway", Region: "nl-ams"}},
			estimate: true,
		},
		{
			name:     "every target with a region for one of them",
			targets:  []string{"all"},
			regions:  []web.Region{{Target: "scaleway", Region: "fr-par"}},
			estimate: true,
		},
		{name: "one target without estimates", targets: []string{"scaleway"}, estimate: false},
		{name: "every target without estimates", targets: []string{"all"}, estimate: false},
		{
			name:          "one target in schema version 2",
			targets:       []string{"scaleway"},
			estimate:      true,
			schemaVersion: "2",
		},
		{
			name:          "every target in schema version 2",
			targets:       []string{"all"},
			estimate:      true,
			schemaVersion: "2",
		},
	}
}

// TestDownloadsMatchTheCLI pins the parity PRD #297 promises: for the same
// inventory and options, the page's Markdown and JSON downloads are byte for
// byte what `shift2eu assess --format markdown` and `--format json` print, for
// every bundled example and the hostile fixture, for one target, several and
// every target, with and without a region and estimates, and in the JSON report's
// schema version 2 as well as the default.
func TestDownloadsMatchTheCLI(t *testing.T) {
	t.Parallel()

	assessor := newAssessor(t)

	for _, path := range parityInventories(t) {
		document := readInventory(t, path)
		// Both sides name the inventory by its base name: the page is given it,
		// the CLI reads the file.
		name := filepath.Base(path)

		for _, option := range downloadOptions() {
			t.Run(fixtureName(path)+"/"+option.name, func(t *testing.T) {
				t.Parallel()

				result, err := assessor.Assess(t.Context(), document, web.Options{
					Targets:       option.targets,
					Regions:       option.regions,
					Estimate:      option.estimate,
					SchemaVersion: option.schemaVersion,
					InventoryName: name,
				})
				if err != nil {
					t.Fatalf("the page refused %s: %v", path, err)
				}

				for _, format := range []string{markdownFormat, jsonFormat} {
					// --schema-version is the JSON report's alone: the CLI refuses it
					// beside another format, so the Markdown comparison is given none.
					schemaVersion := option.schemaVersion
					if format != jsonFormat {
						schemaVersion = ""
					}

					args := assessArgs(path, optionCase{
						targets:       option.targets,
						regions:       option.regions,
						estimate:      option.estimate,
						schemaVersion: schemaVersion,
						format:        format,
					})
					outcome := runCLI(t, args...)
					if outcome.exit != 0 {
						t.Fatalf("shift2eu %s = exit %d, want 0; message: %q",
							strings.Join(args, " "), outcome.exit, outcome.message)
					}

					want := outcome.stdout
					got := result.Markdown
					if format == jsonFormat {
						got = result.JSON
					}

					if !bytes.Equal(want, got) {
						t.Errorf("%s download of %s differs from `shift2eu %s` (-CLI +page):\n%s",
							format, path, strings.Join(args, " "), cmpDiff(want, got))
					}
				}
			})
		}
	}
}

// TestARefusedInventoryIsKindInventory pins that a document the page's inventory
// reader refuses is a KindInventory error, and that its message never repeats a
// value from the document: the marker below stands where each case's invalid
// value sits, and the message must name the position and field instead.
func TestARefusedInventoryIsKindInventory(t *testing.T) {
	t.Parallel()

	const marker = "ACCT-MARKER-4242"

	oversized := []byte(`{"schemaVersion":1,"source":"aws","resources":[],"padding":"` + marker + `"` +
		strings.Repeat("A", 10<<20) + `"}`)

	tests := []struct {
		name        string
		document    []byte
		wantMention string
	}{
		{
			name:     "malformed JSON",
			document: []byte(`{"schemaVersion":1,"source":"aws","resources":[` + marker),
		},
		{
			name:     "an unsupported schema version",
			document: []byte(`{"schemaVersion":"` + marker + `","source":"aws","resources":[]}`),
		},
		{
			name: "an unknown field beside the marker",
			document: []byte(`{"schemaVersion":1,"source":"aws","resources":[` +
				`{"id":"` + marker + `","category":"compute.vm","bogus":true}]}`),
			wantMention: "bogus",
		},
		{
			name: "an unknown category value",
			document: []byte(`{"schemaVersion":1,"source":"aws","resources":[` +
				`{"id":"instance-one","category":"compute.vm"},` +
				`{"id":"instance-two","category":"` + marker + `"}]}`),
			wantMention: "category",
		},
		{
			name:     "a document over the reader's size cap",
			document: oversized,
		},
	}

	assessor := newAssessor(t)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := assessor.Assess(t.Context(), tc.document, web.Options{
				Targets:       []string{"scaleway"},
				InventoryName: inventoryFileName,
			})
			if err == nil {
				t.Fatal("the page accepted a document the inventory reader refuses, want a KindInventory error")
			}

			var inventoryErr *web.Error
			if !errors.As(err, &inventoryErr) {
				t.Fatalf("the page returned %T, want a *web.Error", err)
			}

			if inventoryErr.Kind != web.KindInventory {
				t.Errorf("the page returned Kind %v for a refused document, want KindInventory", inventoryErr.Kind)
			}

			if inventoryErr.Error() == "" {
				t.Error("the page returned an empty message, want the reason the document was refused")
			}

			if strings.Contains(inventoryErr.Error(), marker) {
				t.Errorf("the message %q repeats a value from the document, which the reader never does: %q",
					inventoryErr.Error(), marker)
			}

			if tc.wantMention != "" && !strings.Contains(inventoryErr.Error(), tc.wantMention) {
				t.Errorf("the message %q does not name the field at fault (%q), so the page shows no position",
					inventoryErr.Error(), tc.wantMention)
			}
		})
	}
}

// parityInventories returns the inventories the download parity reads: every
// bundled example, discovered in examples/, and the two e2e fixtures.
func parityInventories(t *testing.T) []string {
	t.Helper()

	examples, err := filepath.Glob(filepath.Join(examplesRoot, "*", "inventory.json"))
	if err != nil {
		t.Fatalf("finding the example inventories under %s: %v", examplesRoot, err)
	}

	if len(examples) == 0 {
		t.Fatalf("%s holds no */inventory.json, so the parity below would read no bundled example", examplesRoot)
	}

	return append(examples, parityFixture, hostileFixture)
}

// fixtureName names an inventory fixture in a subtest, by the directory it sits
// in and its file name, so the three examples and the two e2e fixtures stay
// apart.
func fixtureName(path string) string {
	return filepath.Base(filepath.Dir(path)) + "/" + filepath.Base(path)
}
