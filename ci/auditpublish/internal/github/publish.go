package github

import (
	"context"
	"errors"

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
	// check names the text a refused scan names: the report (its body, pull
	// request and commit message) or an issue.
	check publish.FailedCheck
}

// rendering is the report and the templates of one private state.
type rendering struct {
	report    publish.Rendered
	templates publish.Templates
}

// Publish runs the publish subcommand in this order:
//
//  1. LoadResult (the client's audited commit) and LoadPrevious.
//  2. CheckRelease of the result's tag, ListAuditIssues and
//     ListAuditAdvisories.
//  3. publish.PrivateFindings and PlanAdvisories over the same run's
//     listing: two of the App's drafts with one fingerprint are
//     publish.ErrDuplicateDrafts here, before anything is rendered or
//     written (outcome 6).
//  4. The private state: publish.PrivateHandled when the plan reproduced a
//     private finding, otherwise publish.PrivateNone. Render
//     publish.RenderReport and publish.RenderTemplates under it, and, when it
//     is PrivateHandled, under publish.PrivateNotFiled too: the fallback a
//     refused creation publishes. PlanIssues.
//  5. Scan every text before the first write: publish.ScanPublic over each
//     variant's report (with its model fields), pull request title and body
//     and commit message and over each issue (with its model fields);
//     publish.ScanAdvisory over every advisory write's summary and
//     description; gitleaks over each of those texts; CheckCommitRuns over
//     the report's and the issues' model fields. A hit is
//     publish.ErrScanRefused and no write is made.
//  6. Write: FileAdvisories, then PublishReport, then CreateIssue for each
//     planned issue, in the plan's order; the first failure stops the run.
//     A creation GitHub refuses as the run's first advisory write
//     (publish.ErrPrivateNotFiled) makes no further advisory call: nothing
//     was filed, so the run publishes the PrivateNotFiled report and
//     templates and the issues, writes the private findings nowhere, and
//     returns nil with that state (outcome 5). A refusal after an earlier
//     advisory write of the run is publish.ErrGitHub (outcome 8) and stops
//     the run before the report: the earlier drafts stand, and the next
//     run's listing reconciles them.
//
// It returns the counts of a run whose writes all succeeded, and otherwise
// the first error, of one of the core's classes or the library's.
func Publish(ctx context.Context, cfg PublishConfig) (Published, error) {
	c := cfg.Client

	switch {
	case c == nil || c.scope.Commit == "":
		return Published{}, refuse(publish.CheckNoClient, "publish: no publish client", publish.ErrConfig)
	case cfg.Gitleaks == nil:
		return Published{}, refuse(publish.CheckNoScanner, "publish: no scanner", publish.ErrConfig)
	case cfg.Run.Repository != c.scope.Repository || cfg.Run.ID != c.scope.RunID || cfg.Run.Date != c.scope.Date:
		return Published{}, refuse(publish.CheckRunNotClients, "publish: the run is not the client's", publish.ErrConfig)
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

	advisories, err := ListAuditAdvisories(ctx, c)
	if err != nil {
		return Published{}, err
	}

	private, err := publish.PrivateFindings(result, cfg.Key)
	if err != nil {
		return Published{}, err
	}

	plan, err := PlanAdvisories(private, advisories, cfg.Run)
	if err != nil {
		return Published{}, err
	}

	state := publish.PrivateNone
	if plan.Reproduced() {
		state = publish.PrivateHandled
	}

	in := publish.ReportInput{Result: result, Run: cfg.Run, Private: state, Previous: previous}

	primary, err := render(in)
	if err != nil {
		return Published{}, err
	}

	variants := []rendering{primary}
	fallback := primary

	if state == publish.PrivateHandled {
		in.Private = publish.PrivateNotFiled

		if fallback, err = render(in); err != nil {
			return Published{}, err
		}

		variants = append(variants, fallback)
	}

	issues, err := PlanIssues(previous, existing, cfg.Run)
	if err != nil {
		return Published{}, err
	}

	if err := scanAll(ctx, cfg, variants, issues, plan); err != nil {
		return Published{}, err
	}

	chosen := primary

	if err := FileAdvisories(ctx, c, plan); err != nil {
		if !errors.Is(err, publish.ErrPrivateNotFiled) {
			return Published{}, err
		}

		chosen, state = fallback, publish.PrivateNotFiled
	}

	if err := PublishReport(ctx, c, ReportPlan{Report: chosen.report, Templates: chosen.templates}); err != nil {
		return Published{}, err
	}

	for _, issue := range issues {
		if err := CreateIssue(ctx, c, issue); err != nil {
			return Published{}, err
		}
	}

	return Published{Public: chosen.report.PublicTotal, Private: state}, nil
}

// render renders the report and the templates of one private state.
func render(in publish.ReportInput) (rendering, error) {
	report, err := publish.RenderReport(in)
	if err != nil {
		return rendering{}, err
	}

	templates, err := publish.RenderTemplates(in)
	if err != nil {
		return rendering{}, err
	}

	return rendering{report: report, templates: templates}, nil
}

// scanAll scans every text a run would publish, before the first write: the
// core's public scan and gitleaks over each public text of every variant,
// the advisory scan and gitleaks over each advisory text, then the
// commit-run check over every public model field. An advisory text gets no
// encoded-run refusal and no commit-run check: it legitimately holds PoCs,
// crash inputs and hex.
func scanAll(ctx context.Context, cfg PublishConfig, variants []rendering, issues []publish.IssueText, plan AdvisoryPlan) error {
	var texts []scanned

	for _, v := range variants {
		texts = append(texts,
			scanned{text: v.report.Body, fields: v.report.ModelFields, check: publish.CheckScanReport},
			scanned{text: v.templates.PRTitle, check: publish.CheckScanReport},
			scanned{text: v.templates.PRBody, check: publish.CheckScanReport},
			scanned{text: v.templates.CommitMessage, check: publish.CheckScanReport},
		)
	}

	// Both variants render the same model fields, so the first one's stand
	// for every variant's.
	fields := append([]string(nil), variants[0].report.ModelFields...)

	for _, issue := range issues {
		texts = append(texts,
			scanned{text: issue.Title, check: publish.CheckScanIssue},
			scanned{text: issue.Body, fields: issue.ModelFields, check: publish.CheckScanIssue},
		)
		fields = append(fields, issue.ModelFields...)
	}

	for _, s := range texts {
		if err := publish.ScanPublic(s.text, s.fields, cfg.Key); err != nil {
			return publish.Refuse(s.check, err)
		}

		if err := cfg.Gitleaks.Scan(ctx, s.text); err != nil {
			return publish.Refuse(s.check, err)
		}
	}

	for _, w := range plan.writes {
		for _, text := range []string{w.Text.Summary, w.Text.Description} {
			if err := publish.ScanAdvisory(text, cfg.Key); err != nil {
				return publish.Refuse(publish.CheckScanAdvisory, err)
			}

			if err := cfg.Gitleaks.Scan(ctx, text); err != nil {
				return publish.Refuse(publish.CheckScanAdvisory, err)
			}
		}
	}

	// A refused commit-run check names the model fields; GitHub's failure of
	// a lookup keeps the lookup's own check.
	return publish.Refuse(publish.CheckScanModelFields, CheckCommitRuns(ctx, cfg.Client, fields))
}
