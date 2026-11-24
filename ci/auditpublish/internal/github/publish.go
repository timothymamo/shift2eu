package github

import (
	"context"
	"fmt"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// PublishConfig is what Publish needs: the files it reads, the run, the
// fingerprint key, and the gh client and gitleaks scanner its caller built
// (and closes).
type PublishConfig struct {
	// ResultPath is the decrypted model result's absolute path (LoadResult).
	ResultPath string
	// ReportsDir is the checkout's docs/security/audits, absolute
	// (LoadPrevious).
	ReportsDir string
	// Run is the run; its repository, ID and date must be the client's
	// scope's, or Publish refuses it (publish.ErrConfig).
	Run publish.Run
	// Key is AUDIT_FINGERPRINT_KEY's value.
	Key publish.FingerprintKey
	// Client is the publish client: its scope names the audited commit.
	Client *Client
	// Gitleaks scans every text before the first write.
	Gitleaks *Gitleaks
}

// Published is a run's counts, which its summary line states
// (publish.OutcomeOf(nil).WithCounts): the report's public total and the
// private state it was rendered with.
type Published struct {
	Public  int
	Private publish.PrivateState
}

// scanned is one text Publish scans before the first write, and the escaped
// fragments of model text it holds.
type scanned struct {
	text   string
	fields []string
}

// Publish runs the publish subcommand, without advisories (PR 4b), in this
// order:
//
//  1. LoadResult (the client's audited commit) and LoadPrevious.
//  2. CheckRelease of the result's tag, and ListAuditIssues.
//  3. The private state: publish.PrivateNone when no finding is placed
//     private, otherwise publish.PrivateNotFiled, since no advisory side
//     exists yet: a private finding is never filed and never dropped, and the
//     run ends with outcome 5.
//  4. Render every text: publish.RenderReport and publish.RenderTemplates
//     under that state, PlanIssues.
//  5. Scan every text before the first write: publish.ScanPublic over the
//     report (with its model fields), the pull request's title and body, the
//     commit message and each issue (with its model fields); gitleaks over
//     each of those texts; CheckCommitRuns over the report's and the issues'
//     model fields. A hit is publish.ErrScanRefused and no write is made.
//  6. Write: PublishReport, then CreateIssue for each planned issue, in the
//     plan's order; the first failure stops the run.
//
// It returns the counts of a run whose writes all succeeded, and otherwise
// the first error, of one of the core's classes or the library's.
func Publish(ctx context.Context, cfg PublishConfig) (Published, error) {
	c := cfg.Client

	switch {
	case c == nil || c.scope.Commit == "":
		return Published{}, fmt.Errorf("publish: no publish client: %w", publish.ErrConfig)
	case cfg.Gitleaks == nil:
		return Published{}, fmt.Errorf("publish: no scanner: %w", publish.ErrConfig)
	case cfg.Run.Repository != c.scope.Repository || cfg.Run.ID != c.scope.RunID || cfg.Run.Date != c.scope.Date:
		return Published{}, fmt.Errorf("publish: the run is not the client's: %w", publish.ErrConfig)
	}

	result, err := LoadResult(cfg.ResultPath, c.scope.Commit)
	if err != nil {
		return Published{}, err
	}

	previous, err := LoadPrevious(cfg.ReportsDir)
	if err != nil {
		return Published{}, err
	}

	if err := CheckRelease(ctx, c, result.ReleaseTag); err != nil {
		return Published{}, err
	}

	existing, err := ListAuditIssues(ctx, c)
	if err != nil {
		return Published{}, err
	}

	private := publish.PrivateNone

	for _, f := range result.Findings {
		if publish.Place(f).Private() {
			private = publish.PrivateNotFiled
		}
	}

	in := publish.ReportInput{Result: result, Run: cfg.Run, Private: private, Previous: previous}

	report, err := publish.RenderReport(in)
	if err != nil {
		return Published{}, err
	}

	templates, err := publish.RenderTemplates(in)
	if err != nil {
		return Published{}, err
	}

	issues, err := PlanIssues(previous, existing, cfg.Run)
	if err != nil {
		return Published{}, err
	}

	if err := scanAll(ctx, cfg, report, templates, issues); err != nil {
		return Published{}, err
	}

	if err := PublishReport(ctx, c, ReportPlan{Report: report, Templates: templates}); err != nil {
		return Published{}, err
	}

	for _, issue := range issues {
		if err := CreateIssue(ctx, c, issue); err != nil {
			return Published{}, err
		}
	}

	return Published{Public: report.PublicTotal, Private: private}, nil
}

// scanAll scans every text a run would publish, before the first write: the
// core's public scan and gitleaks over each text, then the commit-run check
// over every model field.
func scanAll(ctx context.Context, cfg PublishConfig, report publish.Rendered, templates publish.Templates, issues []publish.IssueText) error {
	texts := []scanned{
		{text: report.Body, fields: report.ModelFields},
		{text: templates.PRTitle},
		{text: templates.PRBody},
		{text: templates.CommitMessage},
	}

	fields := append([]string(nil), report.ModelFields...)

	for _, issue := range issues {
		texts = append(texts, scanned{text: issue.Title}, scanned{text: issue.Body, fields: issue.ModelFields})
		fields = append(fields, issue.ModelFields...)
	}

	for _, s := range texts {
		if err := publish.ScanPublic(s.text, s.fields, cfg.Key); err != nil {
			return err
		}

		if err := cfg.Gitleaks.Scan(ctx, s.text); err != nil {
			return err
		}
	}

	return CheckCommitRuns(ctx, cfg.Client, fields)
}
