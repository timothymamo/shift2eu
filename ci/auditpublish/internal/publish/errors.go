package publish

import "errors"

// The classes of error the core returns and the entry point classifies. An
// error wraps exactly the class it belongs to; its text names a field's path
// and the rule it broke, never a value, and OutcomeOf reads only its class.
var (
	// ErrResultInvalid is a model result the schema refuses (ADR 0040, "The
	// result schema"), and a result that does not decrypt.
	ErrResultInvalid = errors.New("result invalid")

	// ErrPreviousReport is a previous report the core cannot read exactly as
	// it renders one.
	ErrPreviousReport = errors.New("previous report unreadable")

	// ErrScanRefused is a text the secret and encoded-token scan refuses.
	ErrScanRefused = errors.New("scan refused")

	// ErrConfig is a value of the run's configuration the core refuses: the
	// fingerprint key, the run's identity, the commit argument.
	ErrConfig = errors.New("configuration invalid")

	// ErrPrivateNotFiled is the entry point's: the report was published, but
	// GitHub refused the run's first advisory write, a creation, so no draft
	// advisory was filed (outcome 5).
	ErrPrivateNotFiled = errors.New("private findings not filed")

	// ErrDuplicateDrafts is the entry point's: two of the audit App's draft
	// advisories carry one fingerprint (outcome 6).
	ErrDuplicateDrafts = errors.New("duplicate draft advisories")

	// ErrGitHub is the entry point's: a GitHub call failed (outcome 8).
	ErrGitHub = errors.New("GitHub call failed")
)
