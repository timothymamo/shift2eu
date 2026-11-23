package livecheck

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// MaxRecordBytes caps how much of a record ParseHSTSRecord and
// ParsePreloadRecord read: a longer input is an error.
const MaxRecordBytes = 4 << 10

// RecordFormat is the one format of both records this program reads, their
// "format" field. The format is owned by the hosting ADR (docs/adr/0042, "The
// HSTS and preload records").
const RecordFormat = 1

// MaxReasonRunes caps hsts.json's "reason", in printable characters.
const MaxReasonRunes = 500

// recordDateLayout is the layout of the records' dates, YYYY-MM-DD.
const recordDateLayout = "2006-01-02"

// recordDatePattern is the exact shape of a record's date; time.Parse then
// refuses a day the month does not have.
var recordDatePattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)

// HSTSOutcome is site/records/hsts.json's "outcome".
type HSTSOutcome string

// The two outcomes of the HSTS record.
const (
	// HSTSMet: HSTS is in force; the host's header is held to the minimum.
	HSTSMet HSTSOutcome = "met"
	// HSTSWaived: the maintainer waived HSTS; the pages must say so.
	HSTSWaived HSTSOutcome = "waived"
)

// HSTSRecord is site/records/hsts.json (ADR 0042):
//
//	{"format": 1, "outcome": "met"|"waived", "preload": bool, "date": "YYYY-MM-DD", "reason": "..."}
//
// Preload is true only with HSTSMet; Reason is empty with HSTSMet and
// required with HSTSWaived.
type HSTSRecord struct {
	Outcome HSTSOutcome
	Preload bool
	Date    string
	Reason  string
}

// PreloadRecord is site/records/preload.json (ADR 0042):
//
//	{"format": 1, "listed": bool, "submitted": "YYYY-MM-DD"|"", "checked": "YYYY-MM-DD", "subdomains": [...]}
//
// Listed true needs Submitted; Subdomains are host names under DefaultApex,
// sorted, each once, and empty while Submitted is. Listed counts only when
// hsts.json pursues preload too (Records.Listed).
type PreloadRecord struct {
	Listed     bool
	Submitted  string
	Checked    string
	Subdomains []string
}

// ParseHSTSRecord reads hsts.json strictly from r, at most MaxRecordBytes
// (more is an error): exactly one JSON object with exactly the five fields
// above, each present once, its key exactly as written (lower case), of its
// type (a JSON null is not a value), "format" equal to RecordFormat, "outcome"
// one of its constants, "preload" true only when met, "date" a real calendar
// day as YYYY-MM-DD, "reason" at most MaxReasonRunes printable characters,
// empty when met and not empty when waived, and nothing after the object but
// white space.
func ParseHSTSRecord(r io.Reader) (HSTSRecord, error) {
	var doc struct {
		Format  *int    `json:"format"`
		Outcome *string `json:"outcome"`
		Preload *bool   `json:"preload"`
		Date    *string `json:"date"`
		Reason  *string `json:"reason"`
	}

	if err := decodeStrict(r, MaxRecordBytes, &doc, "format", "outcome", "preload", "date", "reason"); err != nil {
		return HSTSRecord{}, fmt.Errorf("hsts.json: %w", err)
	}

	if err := checkFormat("hsts.json", doc.Format); err != nil {
		return HSTSRecord{}, err
	}

	if doc.Outcome == nil || doc.Preload == nil || doc.Date == nil || doc.Reason == nil {
		return HSTSRecord{}, errors.New(`hsts.json: "outcome", "preload", "date" and "reason" are all required`)
	}

	record := HSTSRecord{
		Outcome: HSTSOutcome(*doc.Outcome),
		Preload: *doc.Preload,
		Date:    *doc.Date,
		Reason:  *doc.Reason,
	}

	if err := record.check(); err != nil {
		return HSTSRecord{}, fmt.Errorf("hsts.json: %w", err)
	}

	return record, nil
}

// check returns nil when the values of a decoded hsts.json meet its rules.
func (h HSTSRecord) check() error {
	switch h.Outcome {
	case HSTSMet:
		if h.Reason != "" {
			return errors.New(`"reason" is not empty while the outcome is met`)
		}
	case HSTSWaived:
		if h.Preload {
			return errors.New(`"preload" is true while the outcome is waived`)
		}

		if h.Reason == "" {
			return errors.New(`"reason" is empty while the outcome is waived`)
		}
	default:
		return fmt.Errorf("outcome %q, want %q or %q", h.Outcome, HSTSMet, HSTSWaived)
	}

	if err := checkDate("date", h.Date); err != nil {
		return err
	}

	return checkReason(h.Reason)
}

// ParsePreloadRecord reads preload.json strictly from r, under the same rules
// as ParseHSTSRecord: exactly the fields "format" (RecordFormat), "listed" (a
// boolean), "submitted" (YYYY-MM-DD, or empty when never submitted, but not
// while listed), "checked" (YYYY-MM-DD) and "subdomains" (host names under
// DefaultApex, in lower case, sorted, each once, and none while nothing was
// submitted).
func ParsePreloadRecord(r io.Reader) (PreloadRecord, error) {
	var doc struct {
		Format     *int      `json:"format"`
		Listed     *bool     `json:"listed"`
		Submitted  *string   `json:"submitted"`
		Checked    *string   `json:"checked"`
		Subdomains *[]string `json:"subdomains"`
	}

	if err := decodeStrict(r, MaxRecordBytes, &doc, "format", "listed", "submitted", "checked", "subdomains"); err != nil {
		return PreloadRecord{}, fmt.Errorf("preload.json: %w", err)
	}

	if err := checkFormat("preload.json", doc.Format); err != nil {
		return PreloadRecord{}, err
	}

	if doc.Listed == nil || doc.Submitted == nil || doc.Checked == nil || doc.Subdomains == nil {
		return PreloadRecord{}, errors.New(`preload.json: "listed", "submitted", "checked" and "subdomains" are all required`)
	}

	record := PreloadRecord{
		Listed:     *doc.Listed,
		Submitted:  *doc.Submitted,
		Checked:    *doc.Checked,
		Subdomains: *doc.Subdomains,
	}

	if err := record.check(); err != nil {
		return PreloadRecord{}, fmt.Errorf("preload.json: %w", err)
	}

	return record, nil
}

// check returns nil when the values of a decoded preload.json meet its rules.
func (p PreloadRecord) check() error {
	if err := checkDate("checked", p.Checked); err != nil {
		return err
	}

	if p.Submitted == "" {
		switch {
		case p.Listed:
			return errors.New(`"listed" is true with no "submitted" date`)
		case len(p.Subdomains) > 0:
			return errors.New(`"subdomains" is not empty while nothing was submitted`)
		}

		return nil
	}

	if err := checkDate("submitted", p.Submitted); err != nil {
		return err
	}

	return checkSubdomains(p.Subdomains)
}

// checkFormat returns nil when a record's format is RecordFormat.
func checkFormat(name string, format *int) error {
	if format == nil {
		return fmt.Errorf("%s: \"format\" is required", name)
	}

	if *format != RecordFormat {
		return fmt.Errorf("%s: format %d, want %d", name, *format, RecordFormat)
	}

	return nil
}

// checkDate returns nil when value, the record field field, is a calendar day
// written YYYY-MM-DD.
func checkDate(field, value string) error {
	if !recordDatePattern.MatchString(value) {
		return fmt.Errorf("%q %q is not a date written YYYY-MM-DD", field, value)
	}

	if _, err := time.Parse(recordDateLayout, value); err != nil {
		return fmt.Errorf("%q %q is not a calendar day: %w", field, value, err)
	}

	return nil
}

// checkReason returns nil when reason is at most MaxReasonRunes printable
// characters: no control character, line break or tab.
func checkReason(reason string) error {
	if !utf8.ValidString(reason) {
		return errors.New(`"reason" is not valid UTF-8`)
	}

	if n := utf8.RuneCountInString(reason); n > MaxReasonRunes {
		return fmt.Errorf(`"reason" has %d characters, want at most %d`, n, MaxReasonRunes)
	}

	for _, c := range reason {
		if !unicode.IsPrint(c) {
			return fmt.Errorf(`"reason" holds the character %U, which is not printable`, c)
		}
	}

	return nil
}

// checkSubdomains returns nil when each of subdomains is a host name under
// DefaultApex, in lower case, and they are sorted with none twice. The record
// is tied to the production domain shift2eu.com (ADR 0042) whatever -apex a
// run is given, so a test run with another apex refuses a listed subdomain
// and fails safe.
func checkSubdomains(subdomains []string) error {
	for i, name := range subdomains {
		if !isDNSName(name) || !strings.HasSuffix(name, "."+DefaultApex) {
			return fmt.Errorf("subdomain %q is not a lower-case host name under %s", name, DefaultApex)
		}

		if i > 0 && subdomains[i-1] >= name {
			return fmt.Errorf("subdomain %q is not after %q: want them sorted, each once", name, subdomains[i-1])
		}
	}

	return nil
}

// Records are both records as the caller read them. A record whose file is
// missing or unreadable, or which its parser refused, has its error set and
// its value ignored: Run then fails safe (behaviour 8). An HSTS record with
// an error fails a KindHSTS check (it never counts as waived or met). A preload
// record with an error counts as not on the preload list, so privacy.html must
// carry the downgrade-risk statement, and also fails a KindHSTS check of its
// own, so an unreadable record never passes silently.
//
// The two records meet in one rule (ADR 0042): preload.json's listed true
// counts only with an hsts.json that was read and pursues preload (met, preload
// true). Otherwise Listed is false, the downgrade-risk statement is required,
// and the claim fails a KindHSTS check at preload.json.
type Records struct {
	HSTS       HSTSRecord
	HSTSErr    error
	Preload    PreloadRecord
	PreloadErr error
}

// Listed reports whether the site counts as on the HSTS preload list: both
// records were read, preload.json says listed, and hsts.json pursues preload.
func (rs Records) Listed() bool {
	return rs.PreloadErr == nil && rs.Preload.Listed && rs.pursuesPreload()
}

// pursuesPreload reports whether hsts.json was read and pursues preload.
func (rs Records) pursuesPreload() bool {
	return rs.HSTSErr == nil && rs.HSTS.Outcome == HSTSMet && rs.HSTS.Preload
}

// listedWithoutPreload returns the error of a preload.json that claims the
// preload list while hsts.json does not back it, or nil.
func (rs Records) listedWithoutPreload() error {
	if rs.PreloadErr != nil || !rs.Preload.Listed || rs.pursuesPreload() {
		return nil
	}

	if rs.HSTSErr != nil {
		return errors.New(`"listed" is true but hsts.json could not be read, so preload is not known to be pursued (fail safe, counted as not on the preload list)`)
	}

	return errors.New(`"listed" is true but hsts.json does not pursue preload ("outcome" met and "preload" true) (fail safe, counted as not on the preload list)`)
}
