package analysis

import (
	"errors"
	"fmt"
	"strings"

	"github.com/timothymamo/shift2eu/internal/domain/text"
)

// The validated option rules of an assessment run (PRD #297): which targets a
// run selects, a region per selected target, the report format and the JSON
// report's schemaVersion, and which combinations of them are allowed. Every
// driving adapter that lets a user make these choices (internal/cli and
// internal/web) parses its own input (flags, values from the page) and checks
// it here, so that both accept the same choices and refuse the rest with the
// same messages. The messages use the CLI's vocabulary (--target, --region,
// --format, --schema-version), which the page shows as it is.
//
// Every error the rules return is a usage error: errors.Is(err,
// ErrInvalidChoice) holds for it. Values the user gave are shown through
// text.Quote.

// ErrInvalidChoice is wrapped by every error of the option rules
// (SelectTargets, SelectRegions, SelectFormat, SelectSchemaVersion and
// RegionChoiceError): the user chose targets, regions, a format or a
// schemaVersion the run cannot take. A driving adapter reports it as a usage
// error.
var ErrInvalidChoice = errors.New("invalid choice")

// choiceError is an error of the option rules: msg, wrapping ErrInvalidChoice
// and, when set, err.
type choiceError struct {
	msg string
	err error
}

// Error returns the message.
func (e *choiceError) Error() string { return e.msg }

// Unwrap returns ErrInvalidChoice and the wrapped error, if any.
func (e *choiceError) Unwrap() []error {
	if e.err == nil {
		return []error{ErrInvalidChoice}
	}

	return []error{ErrInvalidChoice, e.err}
}

// invalid returns a *choiceError with the formatted message.
func invalid(format string, args ...any) error {
	return &choiceError{msg: fmt.Sprintf(format, args...)}
}

// AllTargets is the target value that selects every supported target. It
// cannot be combined with another target.
const AllTargets = "all"

// SelectTargets checks the targets a user chose, given in order, against
// supported, the keys of every supported target sorted, and returns the
// selected keys sorted and distinct: one key for a single-target run, several
// for a comparison. given is either the single value AllTargets, which selects
// every supported key, or a list of distinct supported keys.
//
// An empty given, an empty key, AllTargets beside another key, a key given
// more than once and a key not in supported are refused; every such error but
// the first ends with the list of supported targets. An empty key's message
// quotes the whole choice as the CLI's --target value writes it, the keys
// joined by commas.
func SelectTargets(given, supported []string) ([]string, error) {
	if len(given) == 0 {
		return nil, invalid("assess needs --target")
	}

	list := strings.Join(supported, ", ")

	if len(given) == 1 && given[0] == AllTargets {
		return append([]string(nil), supported...), nil
	}

	for i, key := range given {
		switch {
		case key == "":
			return nil, invalid("--target %s has an empty target; supported targets: %s",
				text.Quote(strings.Join(given, ",")), list)
		case key == AllTargets:
			return nil, invalid("--target %s cannot be combined with other targets; supported targets: %s",
				text.Quote(AllTargets), list)
		case contains(given[:i], key):
			return nil, invalid("target %s given more than once; supported targets: %s",
				text.Quote(key), list)
		case !contains(supported, key):
			return nil, invalid("unknown target %s; supported targets: %s", text.Quote(key), list)
		}
	}

	// supported is sorted, so keeping its order sorts the selection.
	selected := make([]string, 0, len(given))

	for _, key := range supported {
		if contains(given, key) {
			selected = append(selected, key)
		}
	}

	return selected, nil
}

// RegionChoice is one region a user chose for one target, as given.
type RegionChoice struct {
	// Target is the key of the target the region is for.
	Target string
	// Region is the region, which the target's catalog must list; that is
	// checked once the catalog is loaded (RegionError).
	Region string
	// Text is the choice as the user wrote it, which a refusal of a malformed
	// choice quotes as it is, such as the CLI's " scaleway=nl-ams" pair with its
	// spaces. An adapter whose user writes no such text gives Target=Region.
	Text string
}

// SelectRegions checks the regions a user chose, in the order given, against
// selected, the target keys SelectTargets returned, and returns the region
// chosen for each target that has one, by target key; nil when given is empty.
// A target without an entry is assessed in all its regions.
//
// A choice with an empty Target or Region is malformed. A choice for a target
// selected does not hold, and a second choice for the same target, are
// refused. Whether the target's catalog lists the region is checked by RunWith
// once the catalog is loaded, before the inventory is read (RegionError,
// RegionChoiceError).
func SelectRegions(given []RegionChoice, selected []string) (map[string]string, error) {
	if len(given) == 0 {
		return nil, nil
	}

	list := strings.Join(selected, ", ")
	regions := make(map[string]string, len(given))

	for _, choice := range given {
		switch {
		case choice.Target == "" || choice.Region == "":
			return nil, invalid("--region %s is not <target>=<region>; selected targets: %s",
				text.Quote(choice.Text), list)
		case !contains(selected, choice.Target):
			return nil, invalid("--region names target %s, which --target does not select; selected targets: %s",
				text.Quote(choice.Target), list)
		}

		if _, ok := regions[choice.Target]; ok {
			return nil, invalid("--region names target %s more than once", text.Quote(choice.Target))
		}

		regions[choice.Target] = choice.Region
	}

	return regions, nil
}

// RegionChoiceError returns the usage error for a *RegionError inside err, a
// region a target's catalog does not list, found by RunWith once the catalog
// is loaded; nil when err holds none. selected are the target keys of the run:
// for a single-target run, whose RegionError names no target, the error names
// the one key. The error wraps ErrInvalidChoice and the *RegionError, so
// errors.Is(err, ErrUnknownRegion) still holds.
func RegionChoiceError(err error, selected []string) error {
	var regionErr *RegionError
	if !errors.As(err, &regionErr) {
		return nil
	}

	named := *regionErr
	if named.Target == "" && len(selected) == 1 {
		named.Target = selected[0]
	}

	return &choiceError{msg: "--region: " + named.Error(), err: &named}
}

// SelectFormat checks the report format a user chose against supported, the
// keys of every supported format sorted.
func SelectFormat(format string, supported []string) error {
	if !contains(supported, format) {
		return invalid("unknown format %s; supported formats: %s",
			text.Quote(format), strings.Join(supported, ", "))
	}

	return nil
}

// The JSON report's versions (docs/adr/0018, issue #307). The application
// imports no adapter, so it names the one format that has a schemaVersion, the
// versions its reporter writes and the one a run writes when the user chooses
// none, here.
const (
	// JSONFormat is the format whose report has a schemaVersion: the JSON
	// report.
	JSONFormat = "json"
	// DefaultSchemaVersion is the schemaVersion a JSON report is written in
	// when the user chooses none.
	DefaultSchemaVersion = 3
)

// schemaVersions are the schemaVersion values a user may choose, in the order
// the errors list them, and the version each selects.
var schemaVersions = []struct {
	value   string
	version int
}{
	{"2", 2},
	{"3", 3},
}

// SelectSchemaVersion returns the schemaVersion a run of format writes: value,
// which must be one of the supported versions (2 and 3), when given is true,
// or DefaultSchemaVersion; 0 for a format other than JSONFormat, which has no
// schemaVersion. Choosing a version for such a format is refused, so a run
// never quietly ignores it.
func SelectSchemaVersion(format, value string, given bool) (int, error) {
	if format != JSONFormat {
		if given {
			return 0, invalid("--schema-version applies only to --format %s, not to --format %s",
				JSONFormat, text.Quote(format))
		}

		return 0, nil
	}

	if !given {
		return DefaultSchemaVersion, nil
	}

	values := make([]string, 0, len(schemaVersions))

	for _, v := range schemaVersions {
		if v.value == value {
			return v.version, nil
		}

		values = append(values, v.value)
	}

	return 0, invalid("--schema-version: unknown schema version %s; supported schema versions: %s",
		text.Quote(value), strings.Join(values, ", "))
}

// contains reports whether list holds s.
func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}

	return false
}
