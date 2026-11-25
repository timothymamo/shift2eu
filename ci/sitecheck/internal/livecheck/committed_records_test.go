package livecheck_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/timothymamo/shift2eu/ci/sitecheck/internal/livecheck"
)

// This file holds the check of the two records as the repository commits them,
// site/records/hsts.json and site/records/preload.json, against the strict
// readers this program reads them with (docs/adr/0042, *The HSTS and preload
// records*). The checks of run_test.go read fixtures, so a record the parsers
// would refuse in the released commit could pass every test here; this file
// reads the commit's own bytes, and pins their values, so that editing a record
// is a deliberate change of this file too.

// The two records' paths, from the repository root.
const (
	hstsRecordPath    = "site/records/hsts.json"
	preloadRecordPath = "site/records/preload.json"
)

// TestTheCommittedRecordsParse pins the two records the commit holds: each
// parses with its strict reader and yields the values docs/adr/0042's decision
// recorded, and together they count the site as not on the HSTS preload list.
// A record edited, or one the readers would refuse, fails here rather than in
// the release's live check.
func TestTheCommittedRecordsParse(t *testing.T) {
	t.Parallel()

	hsts, preload := theCommittedRecords(t)

	t.Run("site/records/hsts.json", func(t *testing.T) {
		t.Parallel()

		want := livecheck.HSTSRecord{
			Outcome: livecheck.HSTSMet,
			Preload: false,
			Date:    "2026-11-25",
			Reason:  "",
		}

		if diff := cmp.Diff(want, hsts); diff != "" {
			t.Errorf("the committed %s is not the record it was committed as (-want +got):\n%s", hstsRecordPath, diff)
		}
	})

	t.Run("site/records/preload.json", func(t *testing.T) {
		t.Parallel()

		want := livecheck.PreloadRecord{
			Listed:     false,
			Submitted:  "",
			Checked:    "2026-11-25",
			Subdomains: []string{},
		}

		// EquateEmpty: whether the reader reports no subdomains as nil or as
		// an empty slice is a representation detail the record does not
		// decide, so the check pins the values and not which of the two the
		// reader happens to return for an empty list.
		if diff := cmp.Diff(want, preload, cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("the committed %s is not the record it was committed as (-want +got):\n%s", preloadRecordPath, diff)
		}
	})

	t.Run("the two records together", func(t *testing.T) {
		t.Parallel()

		records := livecheck.Records{HSTS: hsts, Preload: preload}

		// The cross-record rule (ADR 0042): preload.json's listed counts only
		// with an hsts.json that pursues preload (met, preload true). The
		// committed pair pursues none and claims none, so the rule has nothing
		// to refuse and the site does not count as on the preload list. A
		// records pair that trips the rule fails a KindHSTS check of the run
		// (round.go), which run_test.go pins; what this file pins is that the
		// commit's own pair trips nothing.
		if records.Listed() {
			t.Errorf("the committed records count the site as on the HSTS preload list, but hsts.json is met with preload false and preload.json says listed false")
		}
	})
}

// theCommittedRecords reads both records from the repository and parses them
// with the strict readers, failing the test when either is refused: a record
// this program cannot read decides nothing, and the released commit must carry
// one it can.
func theCommittedRecords(t *testing.T) (livecheck.HSTSRecord, livecheck.PreloadRecord) {
	t.Helper()

	hsts, err := livecheck.ParseHSTSRecord(bytes.NewReader(readCommittedRecord(t, hstsRecordPath)))
	if err != nil {
		t.Fatalf("ParseHSTSRecord(the committed %s) failed: %v, want it accepted", hstsRecordPath, err)
	}

	preload, err := livecheck.ParsePreloadRecord(bytes.NewReader(readCommittedRecord(t, preloadRecordPath)))
	if err != nil {
		t.Fatalf("ParsePreloadRecord(the committed %s) failed: %v, want it accepted", preloadRecordPath, err)
	}

	return hsts, preload
}

// readCommittedRecord returns the bytes of one of the repository's records,
// read from the repository root and never copied into testdata, so the check
// follows an edit of the commit's own file.
func readCommittedRecord(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(theRepositoryRoot(t), filepath.FromSlash(path)))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return data
}

// theRepositoryRoot returns the module root, four directories above the
// package directory go test runs this package in, failing the test when it
// holds no go.mod: a root resolved to the wrong directory must say so rather
// than report a record as missing.
func theRepositoryRoot(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve the module root: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("the module root %s holds no go.mod: %v", root, err)
	}

	return root
}
