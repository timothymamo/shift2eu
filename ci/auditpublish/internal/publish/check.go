package publish

import "errors"

// FailedCheck names one refusal of a publish run in fixed words: the check that
// failed, which the run's summary line names after its class's words (issue
// #640). Its phrase comes from checkPhrases alone, a fixed table in this file,
// never from an error's text, so the line can hold no value, no token, no key,
// no path, no API response and no finding's text (ADR 0040, "Exit codes and
// the summary line"). A FailedCheck outside the table names nothing, and its
// line is the class's alone. (Check is the audit's own check of a result.)
type FailedCheck uint8

// The checks of the core, the GitHub side and the entry point, grouped by the
// class each belongs to. checkPhrases holds each one's words and class.
const (
	checkNone FailedCheck = iota

	// The run's configuration (ErrConfig, exit 9).
	CheckSecretSet
	CheckStepSummary
	CheckUsage
	CheckToken
	CheckServerURL
	CheckServerURLGitHub
	CheckSHA
	CheckRepository
	CheckRepositoryDot
	CheckRunID
	CheckRunDate
	CheckCommitArgument
	CheckFingerprintKey
	CheckNoClient
	CheckNoScanner
	CheckTempDir
	CheckTimeout
	CheckGHConfigDir
	CheckGHConfigDirRemoved
	CheckGHPath
	CheckGHFile
	CheckScopeCommit
	CheckPublishScope
	CheckRunNotClients
	CheckResultPath
	CheckReportsPath
	CheckReportsDir
	CheckReportsListable
	CheckGitleaksPath
	CheckGitleaksHome
	CheckGitleaksHomeRemoved
	CheckJob
	CheckArtifactDir
	CheckArtifactFile

	// The result's content (ErrResultInvalid, exit 3).
	CheckResultSchema
	CheckPrivateStateUnknown
	CheckPrivateStateNone
	CheckPrivateStateSet
	CheckAdvisoryNotPrivate
	CheckAdvisoryFingerprint
	CheckAdvisoryRenderStatus
	CheckAdvisorySetStatus
	CheckAdvisoryStatusLine
	CheckFingerprintTwice
	CheckResultFile
	CheckReleaseTag
	CheckNoTagRelease
	CheckNoLatestRelease
	CheckNotLatestRelease
	CheckImageDigest
	CheckNoAttestation
	CheckNoProvenance

	// A scan (ErrScanRefused, exit 4).
	CheckScanReport
	CheckScanIssue
	CheckScanAdvisory
	CheckScanModelFields

	// The previous report (ErrPreviousReport, exit 7).
	CheckPreviousNotRendered
	CheckPreviousNotRead
	CheckPreviousAfterRun
	CheckPreviousNotBefore
	CheckPreviousFingerprint
	CheckPreviousSeverity
	CheckPreviousCategory
	CheckPreviousStatus
	CheckPreviousNotRegular
	CheckPreviousOverCap
	CheckPreviousUnreadable

	// GitHub's own failure (ErrGitHub, exit 8), named by what the call does.
	CheckListIssues
	CheckCreateIssue
	CheckListAdvisories
	CheckCreateAdvisory
	CheckUpdateAdvisory
	CheckReadTagRelease
	CheckReadLatestRelease
	CheckListAttestations
	CheckListArtifacts
	CheckCommitRuns
	CheckReadReportBranch
	CheckReadAuditedCommit
	CheckWriteBlob
	CheckWriteTree
	CheckWriteCommit
	CheckCompare
	CheckWriteBranch
	CheckOpenPull
	CheckRunGH

	checkEnd
)

// checkPhrases is the fixed list: each check's class and the words the summary
// line names it by. The words are the form of what is wrong, and runbook step
// 11 lists each with its fix.
var checkPhrases = [checkEnd]struct {
	class  error
	phrase string
}{
	CheckSecretSet:           {ErrConfig, "a secret that must not be in this step is set"},
	CheckStepSummary:         {ErrConfig, "the step summary not appendable"},
	CheckUsage:               {ErrConfig, "a bad flag or subcommand"},
	CheckToken:               {ErrConfig, "GH_TOKEN unset or empty"},
	CheckServerURL:           {ErrConfig, "GITHUB_SERVER_URL not https:// and a host name"},
	CheckServerURLGitHub:     {ErrConfig, "GITHUB_SERVER_URL not https://github.com"},
	CheckSHA:                 {ErrConfig, "GITHUB_SHA not 40 lowercase hex"},
	CheckRepository:          {ErrConfig, "GITHUB_REPOSITORY not owner/name"},
	CheckRepositoryDot:       {ErrConfig, "GITHUB_REPOSITORY holds a dot segment"},
	CheckRunID:               {ErrConfig, "GITHUB_RUN_ID not a decimal of up to 20 digits"},
	CheckRunDate:             {ErrConfig, "the run's date not a real YYYY-MM-DD"},
	CheckCommitArgument:      {ErrConfig, "the commit argument not 40 lowercase hex"},
	CheckFingerprintKey:      {ErrConfig, "AUDIT_FINGERPRINT_KEY unset or shorter than 32 bytes"},
	CheckNoClient:            {ErrConfig, "no open client"},
	CheckNoScanner:           {ErrConfig, "no open scanner"},
	CheckTempDir:             {ErrConfig, "the temporary directory not absolute or no directory"},
	CheckTimeout:             {ErrConfig, "a negative timeout"},
	CheckGHConfigDir:         {ErrConfig, "gh's configuration directory not made"},
	CheckGHConfigDirRemoved:  {ErrConfig, "gh's configuration directory not removed"},
	CheckGHPath:              {ErrConfig, "the gh path not absolute"},
	CheckGHFile:              {ErrConfig, "the gh path names no file"},
	CheckScopeCommit:         {ErrConfig, "the scope's commit not 40 lowercase hex"},
	CheckPublishScope:        {ErrConfig, "a publish scope without its commit"},
	CheckRunNotClients:       {ErrConfig, "the run is not the client's"},
	CheckResultPath:          {ErrConfig, "the result's path not absolute and clean"},
	CheckReportsPath:         {ErrConfig, "the reports directory not absolute and clean"},
	CheckReportsDir:          {ErrConfig, "the reports directory no directory"},
	CheckReportsListable:     {ErrConfig, "the reports directory not listable"},
	CheckGitleaksPath:        {ErrConfig, "the gitleaks path not absolute and clean"},
	CheckGitleaksHome:        {ErrConfig, "gitleaks' home directory not made"},
	CheckGitleaksHomeRemoved: {ErrConfig, "gitleaks' home directory not removed"},
	CheckJob:                 {ErrConfig, "the job neither model nor publish"},
	CheckArtifactDir:         {ErrConfig, "the artifact's directory not absolute"},
	CheckArtifactFile:        {ErrConfig, "the artifact file not created"},

	CheckResultSchema:         {ErrResultInvalid, "the result does not match the schema"},
	CheckPrivateStateUnknown:  {ErrResultInvalid, "the private state none of the three"},
	CheckPrivateStateNone:     {ErrResultInvalid, "the private state none, but a finding is private"},
	CheckPrivateStateSet:      {ErrResultInvalid, "the private state set, but no finding is private"},
	CheckAdvisoryNotPrivate:   {ErrResultInvalid, "an advisory for a finding that is not private"},
	CheckAdvisoryFingerprint:  {ErrResultInvalid, "an advisory fingerprint that is no fingerprint"},
	CheckAdvisoryRenderStatus: {ErrResultInvalid, "an advisory status not new, open or regressed"},
	CheckAdvisorySetStatus:    {ErrResultInvalid, "an advisory status none of the four"},
	CheckAdvisoryStatusLine:   {ErrResultInvalid, "an advisory description without a status line"},
	CheckFingerprintTwice:     {ErrResultInvalid, "two findings with one fingerprint"},
	CheckResultFile:           {ErrResultInvalid, "the result not one regular file under the cap"},
	CheckReleaseTag:           {ErrResultInvalid, "the release tag not vX.Y.Z"},
	CheckNoTagRelease:         {ErrResultInvalid, "no release of the tag"},
	CheckNoLatestRelease:      {ErrResultInvalid, "no latest release"},
	CheckNotLatestRelease:     {ErrResultInvalid, "the tag not the latest release's"},
	CheckImageDigest:          {ErrResultInvalid, "the image digest not sha256: and 64 lowercase hex"},
	CheckNoAttestation:        {ErrResultInvalid, "no attestation of the digest"},
	CheckNoProvenance:         {ErrResultInvalid, "no provenance attestation names the digest"},

	CheckScanReport:      {ErrScanRefused, "the report"},
	CheckScanIssue:       {ErrScanRefused, "an issue"},
	CheckScanAdvisory:    {ErrScanRefused, "an advisory"},
	CheckScanModelFields: {ErrScanRefused, "the report's model fields"},

	CheckPreviousNotRendered: {ErrPreviousReport, "the newest report not as the core renders one"},
	CheckPreviousNotRead:     {ErrPreviousReport, "the previous report not read by the core"},
	CheckPreviousAfterRun:    {ErrPreviousReport, "the previous report dated after the run"},
	CheckPreviousNotBefore:   {ErrPreviousReport, "the previous report not before the run"},
	CheckPreviousFingerprint: {ErrPreviousReport, "the previous report's fingerprint not a fingerprint"},
	CheckPreviousSeverity:    {ErrPreviousReport, "the previous report's severity outside the public severities"},
	CheckPreviousCategory:    {ErrPreviousReport, "the previous report's category outside the public categories"},
	CheckPreviousStatus:      {ErrPreviousReport, "the previous report's status not new or open"},
	CheckPreviousNotRegular:  {ErrPreviousReport, "the newest report not a regular file"},
	CheckPreviousOverCap:     {ErrPreviousReport, "the newest report over the cap"},
	CheckPreviousUnreadable:  {ErrPreviousReport, "the newest report not readable"},

	CheckListIssues:        {ErrGitHub, "listing the audit issues"},
	CheckCreateIssue:       {ErrGitHub, "creating the audit issue"},
	CheckListAdvisories:    {ErrGitHub, "listing the audit advisories"},
	CheckCreateAdvisory:    {ErrGitHub, "creating an advisory"},
	CheckUpdateAdvisory:    {ErrGitHub, "updating an advisory"},
	CheckReadTagRelease:    {ErrGitHub, "reading the tag's release"},
	CheckReadLatestRelease: {ErrGitHub, "reading the latest release"},
	CheckListAttestations:  {ErrGitHub, "listing the image's attestations"},
	CheckListArtifacts:     {ErrGitHub, "listing the run's artifacts"},
	CheckCommitRuns:        {ErrGitHub, "looking up the commit's runs"},
	CheckReadReportBranch:  {ErrGitHub, "reading the report's branch"},
	CheckReadAuditedCommit: {ErrGitHub, "reading the audited commit"},
	CheckWriteBlob:         {ErrGitHub, "writing the report's blob"},
	CheckWriteTree:         {ErrGitHub, "writing the report's tree"},
	CheckWriteCommit:       {ErrGitHub, "writing the report's commit"},
	CheckCompare:           {ErrGitHub, "comparing the audited commit with the report's"},
	CheckWriteBranch:       {ErrGitHub, "writing the report's branch"},
	CheckOpenPull:          {ErrGitHub, "opening the report's pull request"},
	CheckRunGH:             {ErrGitHub, "running gh"},
}

// checkError is an error marked with the check that refused it. Its text and
// its classes are the wrapped error's; the check adds only the fixed words the
// summary line names it by.
type checkError struct {
	check FailedCheck
	err   error
}

func (e *checkError) Error() string { return e.err.Error() }

func (e *checkError) Unwrap() error { return e.err }

// Refuse marks err with check c, the check that refused it: the summary line
// of err's class then names c's fixed words. err keeps its text and its class;
// a nil err is nil. A mark whose check belongs to another class than the
// outcome's is passed over, so marking every error a call returns with the
// call's own check names the call only when GitHub's failure is the outcome.
func Refuse(c FailedCheck, err error) error {
	if err == nil {
		return nil
	}

	return &checkError{check: c, err: err}
}

// refusal is the core's own refusal: an error of check c's class whose text
// is detail, marked with c. detail names a field and a rule, never a value.
func refusal(c FailedCheck, detail string) error {
	class := ErrConfig
	if c > checkNone && c < checkEnd {
		class = checkPhrases[c].class
	}

	return Refuse(c, &detailError{detail: detail, class: class})
}

// detailError is a refusal's text and its class.
type detailError struct {
	detail string
	class  error
}

func (e *detailError) Error() string { return e.detail + ": " + e.class.Error() }

func (e *detailError) Unwrap() error { return e.class }

// phraseOf returns the fixed words of the first check that marks err and
// belongs to class, in the order errors.As walks err's tree: depth first, each
// wrapper before what it wraps, a joined error's parts in order. It reports
// false when no mark of that class is found.
func phraseOf(err error, class error) (string, bool) {
	var (
		found string
		ok    bool
	)

	walk(err, func(e error) bool {
		c, marked := e.(*checkError) //nolint:errorlint // walk visits each error of the tree itself, so the assertion sees exactly one node
		if !marked {
			return true
		}

		if c.check > checkNone && c.check < checkEnd && errors.Is(checkPhrases[c.check].class, class) {
			found, ok = checkPhrases[c.check].phrase, true

			return false
		}

		return true
	})

	return found, ok
}

// walk calls visit on err and each error it wraps, depth first, until visit
// returns false.
func walk(err error, visit func(error) bool) bool {
	if err == nil {
		return true
	}

	if !visit(err) {
		return false
	}

	switch u := err.(type) { //nolint:errorlint // walking the tree is what errors.As does itself
	case interface{ Unwrap() error }:
		return walk(u.Unwrap(), visit)
	case interface{ Unwrap() []error }:
		for _, e := range u.Unwrap() {
			if !walk(e, visit) {
				return false
			}
		}
	}

	return true
}
